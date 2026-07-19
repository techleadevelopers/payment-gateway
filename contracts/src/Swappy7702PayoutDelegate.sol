// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

interface IERC20Minimal {
    function transfer(address to, uint256 amount) external returns (bool);
}

library SafeERC20MinimalLite {
    function safeTransfer(IERC20Minimal token, address to, uint256 amount) internal returns (bool) {
        (bool success, bytes memory returndata) = address(token).call(
            abi.encodeWithSelector(IERC20Minimal.transfer.selector, to, amount)
        );
        return success && (returndata.length == 0 || abi.decode(returndata, (bool)));
    }
}

/// @notice Minimal EIP-7702 delegate implementation for controlled payouts.
/// @dev When used through EIP-7702, storage is the EOA storage. Do not add generic execute().
contract Swappy7702PayoutDelegate {
    using SafeERC20MinimalLite for IERC20Minimal;

    bytes32 private constant STORAGE_SLOT = keccak256("swappy.7702.payout.delegate.v1");

    struct DelegateStorage {
        address owner;
        address operator;
        bool paused;
        mapping(address => bool) allowedToken;
        mapping(address => bool) allowedRecipient;
        mapping(address => bool) allowedContractRecipient;
        mapping(bytes32 => bool) executedOperation;
    }

    event Initialized(address indexed owner, address indexed operator);
    event Paused(bool paused);
    event TokenAllowed(address indexed token, bool allowed);
    event RecipientAllowed(address indexed recipient, bool allowed);
    event ContractRecipientAllowed(address indexed recipient, bool allowed);
    event DelegatePayout(bytes32 indexed operationId, address indexed token, address indexed to, uint256 amount);

    error Unauthorized();
    error AlreadyInitialized();
    error PausedError();
    error ZeroAddress();
    error NotAllowed();
    error InvalidAmount();
    error OperationAlreadyExecuted();
    error TransferFailed();
    error ContractRecipientNotAllowed();
    error InvalidOperationId();

    modifier onlyOwner() {
        if (msg.sender != _storage().owner) revert Unauthorized();
        _;
    }

    modifier onlyOperatorOrOwner() {
        DelegateStorage storage ds = _storage();
        if (msg.sender != ds.owner && msg.sender != ds.operator) revert Unauthorized();
        _;
    }

    function initialize(address owner_, address operator_) external {
        if (owner_ == address(0) || operator_ == address(0)) revert ZeroAddress();
        DelegateStorage storage ds = _storage();
        if (ds.owner != address(0)) revert AlreadyInitialized();
        ds.owner = owner_;
        ds.operator = operator_;
        emit Initialized(owner_, operator_);
    }

    function setOperator(address operator_) external onlyOwner {
        if (operator_ == address(0)) revert ZeroAddress();
        _storage().operator = operator_;
    }

    function setPaused(bool paused_) external onlyOwner {
        _storage().paused = paused_;
        emit Paused(paused_);
    }

    function setTokenAllowed(address token, bool allowed) external onlyOwner {
        if (token == address(0)) revert ZeroAddress();
        _storage().allowedToken[token] = allowed;
        emit TokenAllowed(token, allowed);
    }

    function setRecipientAllowed(address recipient, bool allowed) external onlyOwner {
        if (recipient == address(0)) revert ZeroAddress();
        _storage().allowedRecipient[recipient] = allowed;
        emit RecipientAllowed(recipient, allowed);
    }

    function setContractRecipientAllowed(address recipient, bool allowed) external onlyOwner {
        if (recipient == address(0)) revert ZeroAddress();
        if (allowed && recipient.code.length == 0) revert ContractRecipientNotAllowed();
        _storage().allowedContractRecipient[recipient] = allowed;
        emit ContractRecipientAllowed(recipient, allowed);
    }

    function payout(bytes32 operationId, address token, address to, uint256 amount) external onlyOperatorOrOwner {
        DelegateStorage storage ds = _storage();
        if (ds.paused) revert PausedError();
        if (operationId == bytes32(0)) revert InvalidOperationId();
        if (ds.executedOperation[operationId]) revert OperationAlreadyExecuted();
        if (token == address(0) || to == address(0)) revert ZeroAddress();
        if (amount == 0) revert InvalidAmount();
        if (to.code.length != 0 && !ds.allowedContractRecipient[to]) revert ContractRecipientNotAllowed();
        if (!ds.allowedToken[token] || !ds.allowedRecipient[to]) revert NotAllowed();

        ds.executedOperation[operationId] = true;
        if (!IERC20Minimal(token).safeTransfer(to, amount)) revert TransferFailed();
        emit DelegatePayout(operationId, token, to, amount);
    }

    function owner() external view returns (address) {
        return _storage().owner;
    }

    function operator() external view returns (address) {
        return _storage().operator;
    }

    function paused() external view returns (bool) {
        return _storage().paused;
    }

    function isTokenAllowed(address token) external view returns (bool) {
        return _storage().allowedToken[token];
    }

    function isRecipientAllowed(address recipient) external view returns (bool) {
        return _storage().allowedRecipient[recipient];
    }

    function isContractRecipientAllowed(address recipient) external view returns (bool) {
        return _storage().allowedContractRecipient[recipient];
    }

    function _storage() private pure returns (DelegateStorage storage ds) {
        bytes32 slot = STORAGE_SLOT;
        assembly {
            ds.slot := slot
        }
    }
}
