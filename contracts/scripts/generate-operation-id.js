const { ethers } = require("hardhat");

async function main() {
  const chainId = 31337n;
  const vault = "0x1111111111111111111111111111111111111111";
  const intentId = ethers.id("settlement-001");
  const token = "0x2222222222222222222222222222222222222222";
  const recipient = "0x3333333333333333333333333333333333333333";
  const amountRaw = 10000000n;

  const encoded = ethers.AbiCoder.defaultAbiCoder().encode(
    ["uint256", "address", "bytes32", "address", "address", "uint256"],
    [chainId, vault, intentId, token, recipient, amountRaw]
  );

  console.log(ethers.keccak256(encoded));
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
