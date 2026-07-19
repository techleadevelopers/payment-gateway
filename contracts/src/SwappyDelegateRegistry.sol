// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Registry for audited EIP-7702 delegate contracts.
/// @dev The Go signer still validates delegate bytecode hash off-chain.
contract SwappyDelegateRegistry {
    struct DelegateInfo {
        bool trusted;
        bytes32 codeHash;
        string label;
        uint64 addedAt;
        uint64 revokedAt;
    }

    address public owner;
    address public pendingOwner;
    mapping(address => bool) public guardians;
    mapping(address => DelegateInfo) public delegates;

    event OwnershipTransferStarted(address indexed currentOwner, address indexed pendingOwner);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event GuardianSet(address indexed guardian, bool allowed);
    event DelegateTrusted(address indexed delegate, bytes32 indexed codeHash, string label);
    event DelegateRevoked(address indexed delegate, string reason);

    error Unauthorized();
    error ZeroAddress();
    error EmptyCode();
    error HashMismatch();

    modifier onlyOwner() {
        if (msg.sender != owner) revert Unauthorized();
        _;
    }

    modifier onlyGuardianOrOwner() {
        if (msg.sender != owner && !guardians[msg.sender]) revert Unauthorized();
        _;
    }

    constructor(address initialOwner) {
        if (initialOwner == address(0)) revert ZeroAddress();
        owner = initialOwner;
        emit OwnershipTransferred(address(0), initialOwner);
    }

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

    function trustDelegate(address delegate, bytes32 expectedCodeHash, string calldata label) external onlyOwner {
        if (delegate == address(0)) revert ZeroAddress();
        if (delegate.code.length == 0) revert EmptyCode();
        bytes32 observed = delegate.codehash;
        if (expectedCodeHash != bytes32(0) && observed != expectedCodeHash) revert HashMismatch();

        delegates[delegate] = DelegateInfo({
            trusted: true,
            codeHash: observed,
            label: label,
            addedAt: uint64(block.timestamp),
            revokedAt: 0
        });
        emit DelegateTrusted(delegate, observed, label);
    }

    function revokeDelegate(address delegate, string calldata reason) external onlyGuardianOrOwner {
        DelegateInfo storage info = delegates[delegate];
        info.trusted = false;
        info.revokedAt = uint64(block.timestamp);
        emit DelegateRevoked(delegate, reason);
    }

    function isTrusted(address delegate, bytes32 expectedCodeHash) external view returns (bool) {
        DelegateInfo memory info = delegates[delegate];
        if (!info.trusted) return false;
        if (expectedCodeHash != bytes32(0) && info.codeHash != expectedCodeHash) return false;
        return delegate.codehash == info.codeHash;
    }
}
