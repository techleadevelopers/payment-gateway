// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

interface IERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
}

/// @notice Treasury/payout vault for EVM stablecoin operations.
/// @dev Designed for USDT/USDC style ERC20 custody on BSC, Polygon, and compatible EVM networks. Keep business pricing off-chain.
contract SwappyTreasuryVault {
    struct TokenPolicy {
        bool allowed;
        uint256 maxTransfer;
        uint256 dailyLimit;
        uint256 spentToday;
        uint64 day;
    }

    address public owner;
    address public pendingOwner;
    bool public paused;

    mapping(address => bool) public guardians;
    mapping(address => bool) public operators;
    mapping(address => bool) public allowedRecipients;
    mapping(address => bool) public blockedRecipients;
    mapping(address => TokenPolicy) public tokenPolicies;
    mapping(bytes32 => bool) public executedOperation;

    event OwnershipTransferStarted(address indexed currentOwner, address indexed pendingOwner);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event GuardianSet(address indexed guardian, bool allowed);
    event OperatorSet(address indexed operator, bool allowed);
    event RecipientAllowed(address indexed recipient, bool allowed);
    event RecipientBlocked(address indexed recipient, bool blocked);
    event TokenPolicySet(address indexed token, bool allowed, uint256 maxTransfer, uint256 dailyLimit);
    event Paused(address indexed account);
    event Unpaused(address indexed account);
    event Payout(bytes32 indexed operationId, address indexed token, address indexed to, uint256 amount, address operator);
    event TreasuryWithdraw(bytes32 indexed operationId, address indexed token, address indexed to, uint256 amount);
    event NativeWithdraw(bytes32 indexed operationId, address indexed to, uint256 amount);

    error Unauthorized();
    error PausedError();
    error ZeroAddress();
    error InvalidAmount();
    error TokenNotAllowed();
    error RecipientNotAllowed();
    error RecipientIsBlocked();
    error TransferTooLarge();
    error DailyLimitExceeded();
    error OperationAlreadyExecuted();
    error TokenTransferFailed();
    error NativeTransferFailed();

    modifier onlyOwner() {
        if (msg.sender != owner) revert Unauthorized();
        _;
    }

    modifier onlyGuardianOrOwner() {
        if (msg.sender != owner && !guardians[msg.sender]) revert Unauthorized();
        _;
    }

    modifier onlyOperatorOrOwner() {
        if (msg.sender != owner && !operators[msg.sender]) revert Unauthorized();
        _;
    }

    modifier whenNotPaused() {
        if (paused) revert PausedError();
        _;
    }

    constructor(address initialOwner) {
        if (initialOwner == address(0)) revert ZeroAddress();
        owner = initialOwner;
        emit OwnershipTransferred(address(0), initialOwner);
    }

    receive() external payable {}

    function startOwnershipTransfer(address nextOwner) external onlyOwner {
        if (nextOwner == address(0)) revert ZeroAddress();
        pendingOwner = nextOwner;
        emit OwnershipTransferStarted(owner, nextOwner);
    }

    function acceptOwnership() external {
        if (msg.sender != pendingOwner) revert Unauthorized();
        address previous = owner;
        owner = pendingOwner;
        pendingOwner = address(0);
        emit OwnershipTransferred(previous, owner);
    }

    function setGuardian(address account, bool allowed) external onlyOwner {
        if (account == address(0)) revert ZeroAddress();
        guardians[account] = allowed;
        emit GuardianSet(account, allowed);
    }

    function setOperator(address account, bool allowed) external onlyOwner {
        if (account == address(0)) revert ZeroAddress();
        operators[account] = allowed;
        emit OperatorSet(account, allowed);
    }

    function setRecipientAllowed(address recipient, bool allowed) external onlyOwner {
        if (recipient == address(0)) revert ZeroAddress();
        allowedRecipients[recipient] = allowed;
        emit RecipientAllowed(recipient, allowed);
    }

    function setRecipientBlocked(address recipient, bool blocked) external onlyGuardianOrOwner {
        if (recipient == address(0)) revert ZeroAddress();
        blockedRecipients[recipient] = blocked;
        emit RecipientBlocked(recipient, blocked);
    }

    function setTokenPolicy(address token, bool allowed, uint256 maxTransfer, uint256 dailyLimit) external onlyOwner {
        if (token == address(0)) revert ZeroAddress();
        TokenPolicy storage policy = tokenPolicies[token];
        policy.allowed = allowed;
        policy.maxTransfer = maxTransfer;
        policy.dailyLimit = dailyLimit;
        emit TokenPolicySet(token, allowed, maxTransfer, dailyLimit);
    }

    function pause() external onlyGuardianOrOwner {
        paused = true;
        emit Paused(msg.sender);
    }

    function unpause() external onlyOwner {
        paused = false;
        emit Unpaused(msg.sender);
    }

    function payout(bytes32 operationId, address token, address to, uint256 amount) external onlyOperatorOrOwner whenNotPaused {
        _executeTokenOutflow(operationId, token, to, amount, true);
        emit Payout(operationId, token, to, amount, msg.sender);
    }

    function batchPayout(
        bytes32[] calldata operationIds,
        address token,
        address[] calldata recipients,
        uint256[] calldata amounts
    ) external onlyOperatorOrOwner whenNotPaused {
        uint256 length = operationIds.length;
        if (length != recipients.length || length != amounts.length) revert InvalidAmount();
        for (uint256 i = 0; i < length; i++) {
            _executeTokenOutflow(operationIds[i], token, recipients[i], amounts[i], true);
            emit Payout(operationIds[i], token, recipients[i], amounts[i], msg.sender);
        }
    }

    function withdrawToTreasury(bytes32 operationId, address token, address to, uint256 amount) external onlyOwner {
        _executeTokenOutflow(operationId, token, to, amount, false);
        emit TreasuryWithdraw(operationId, token, to, amount);
    }

    function withdrawNative(bytes32 operationId, address payable to, uint256 amount) external onlyOwner {
        if (operationId == bytes32(0)) revert OperationAlreadyExecuted();
        if (executedOperation[operationId]) revert OperationAlreadyExecuted();
        if (to == address(0)) revert ZeroAddress();
        if (amount == 0) revert InvalidAmount();
        executedOperation[operationId] = true;
        (bool ok, ) = to.call{value: amount}("");
        if (!ok) revert NativeTransferFailed();
        emit NativeWithdraw(operationId, to, amount);
    }

    function _executeTokenOutflow(bytes32 operationId, address token, address to, uint256 amount, bool requireAllowedRecipient) internal {
        if (operationId == bytes32(0)) revert OperationAlreadyExecuted();
        if (executedOperation[operationId]) revert OperationAlreadyExecuted();
        if (token == address(0) || to == address(0)) revert ZeroAddress();
        if (amount == 0) revert InvalidAmount();
        if (blockedRecipients[to]) revert RecipientIsBlocked();
        if (requireAllowedRecipient && !allowedRecipients[to]) revert RecipientNotAllowed();

        TokenPolicy storage policy = tokenPolicies[token];
        if (!policy.allowed) revert TokenNotAllowed();
        if (policy.maxTransfer > 0 && amount > policy.maxTransfer) revert TransferTooLarge();
        _applyDailyLimit(policy, amount);

        executedOperation[operationId] = true;
        if (!IERC20(token).transfer(to, amount)) revert TokenTransferFailed();
    }

    function _applyDailyLimit(TokenPolicy storage policy, uint256 amount) internal {
        uint64 currentDay = uint64(block.timestamp / 1 days);
        if (policy.day != currentDay) {
            policy.day = currentDay;
            policy.spentToday = 0;
        }
        if (policy.dailyLimit > 0 && policy.spentToday + amount > policy.dailyLimit) revert DailyLimitExceeded();
        policy.spentToday += amount;
    }
}
