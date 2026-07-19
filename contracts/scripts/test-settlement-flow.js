const { ethers } = require("hardhat");

async function main() {
  const [owner, operator, guardian, recipient] = await ethers.getSigners();

  const Token = await ethers.getContractFactory("MockBEP20");
  const token = await Token.deploy("Mock USDT", "USDT", 6);
  await token.waitForDeployment();

  const Vault = await ethers.getContractFactory("SwappyTreasuryVault");
  const vault = await Vault.deploy(owner.address);
  await vault.waitForDeployment();

  const tokenAddress = await token.getAddress();
  const vaultAddress = await vault.getAddress();
  const amount = ethers.parseUnits("10", 6);

  await vault.connect(owner).setGuardian(guardian.address, true);
  await vault.connect(owner).setOperator(operator.address, true);
  await vault.connect(owner).setTokenPolicy(
    tokenAddress,
    true,
    ethers.parseUnits("100", 6),
    ethers.parseUnits("1000", 6)
  );
  await vault.connect(owner).setRecipientAllowed(recipient.address, true);
  await token.mint(vaultAddress, ethers.parseUnits("1000", 6));

  const network = await ethers.provider.getNetwork();
  const settlementIntentId = ethers.id("settlement-001");
  const operationId = ethers.keccak256(
    ethers.AbiCoder.defaultAbiCoder().encode(
      ["uint256", "address", "bytes32", "address", "address", "uint256"],
      [network.chainId, vaultAddress, settlementIntentId, tokenAddress, recipient.address, amount]
    )
  );

  await vault.connect(operator).payout(operationId, tokenAddress, recipient.address, amount);

  console.log("Vault:", vaultAddress);
  console.log("Token:", tokenAddress);
  console.log("OperationID:", operationId);
  console.log("Recipient balance:", ethers.formatUnits(await token.balanceOf(recipient.address), 6));

  try {
    await vault.connect(operator).payout(operationId, tokenAddress, recipient.address, amount);
    throw new Error("Replay deveria ter falhado");
  } catch (error) {
    if (String(error.message || error).includes("Replay deveria")) {
      throw error;
    }
    console.log("Replay bloqueado corretamente");
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
