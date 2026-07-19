const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("SwappyDelegateRegistry", function () {
  it("trusts and revokes a delegate by codehash", async function () {
    const [owner, guardian] = await ethers.getSigners();

    const Delegate = await ethers.getContractFactory("Swappy7702PayoutDelegate");
    const delegate = await Delegate.deploy();
    await delegate.waitForDeployment();
    const delegateAddress = await delegate.getAddress();
    const code = await ethers.provider.getCode(delegateAddress);
    const codeHash = ethers.keccak256(code);

    const Registry = await ethers.getContractFactory("SwappyDelegateRegistry");
    const registry = await Registry.deploy(owner.address);
    await registry.waitForDeployment();

    await registry.setGuardian(guardian.address, true);
    await expect(registry.trustDelegate(delegateAddress, codeHash, "payout-v1"))
      .to.emit(registry, "DelegateTrusted");

    expect(await registry.isTrusted(delegateAddress, codeHash)).to.equal(true);

    await expect(registry.connect(guardian).revokeDelegate(delegateAddress, "incident"))
      .to.emit(registry, "DelegateRevoked");
    expect(await registry.isTrusted(delegateAddress, codeHash)).to.equal(false);
  });

  it("rejects trusting an address without contract code", async function () {
    const [owner, eoa] = await ethers.getSigners();

    const Registry = await ethers.getContractFactory("SwappyDelegateRegistry");
    const registry = await Registry.deploy(owner.address);
    await registry.waitForDeployment();

    await expect(registry.trustDelegate(eoa.address, ethers.ZeroHash, "eoa"))
      .to.be.revertedWithCustomError(registry, "EmptyCode");
  });
});
