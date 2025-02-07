pragma solidity ^0.8.17;

import "@openzeppelin/contracts/security/ReentrancyGuard.sol";

/// @title UserWallet
/// @notice A wallet contract that allows the owner to withdraw funds and
/// permits whitelisted addresses to deduct fees.
contract UserWallet is ReentrancyGuard {
    address public immutable owner;
    mapping(address => bool) public whitelist;

    event WhitelistAdded(address indexed account);
    event WhitelistRemoved(address indexed account);
    event Withdraw(uint256 amount, address indexed to);
    event DeductFee(uint256 amount, address indexed caller);

    modifier onlyOwner() {
        require(msg.sender == owner, "Not owner");
        _;
    }

    modifier onlyWhitelisted() {
        require(whitelist[msg.sender], "Caller is not whitelisted");
        _;
    }

    constructor(address _owner) payable {
        owner = _owner;
    }

    receive() external payable {}

    function deposit() external payable {}

    /// @notice Withdraws a specified amount of ETH to the owner's address.
    /// @param amount The amount of ETH to withdraw.
    function withdraw(uint256 amount) external onlyOwner nonReentrant {
        require(address(this).balance >= amount, "Insufficient balance");
        (bool sent, ) = owner.call{value: amount}("");
        require(sent, "Transfer failed");
        emit Withdraw(amount, owner);
    }

    /// @notice Allows a whitelisted address to deduct a fee from the wallet.
    /// @param amount The fee amount to deduct.
    function deductFee(uint256 amount) external onlyWhitelisted nonReentrant {
        require(address(this).balance >= amount, "Insufficient balance");
        (bool sent, ) = msg.sender.call{value: amount}("");
        require(sent, "Transfer failed");
        emit DeductFee(amount, msg.sender);
    }

    /// @notice Adds an address to the whitelist.
    /// @param account The address to add to the whitelist.
    function addToWhitelist(address account) external onlyOwner {
        require(account != address(0), "Invalid address");
        whitelist[account] = true;
        emit WhitelistAdded(account);
    }

    /// @notice Removes an address from the whitelist.
    /// @param account The address to remove from the whitelist.
    function removeFromWhitelist(address account) external onlyOwner {
        whitelist[account] = false;
        emit WhitelistRemoved(account);
    }

    function getBalance() external view returns (uint256) {
        return address(this).balance;
    }
}
