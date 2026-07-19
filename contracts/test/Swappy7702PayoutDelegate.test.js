const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("Swappy7702PayoutDelegate", function () {
  async function deployFixture() {
    const [owner, operator, customer, attacker] = await ethers.getSigners();

    const Token = await ethers.getContractFactory("MockBEP20");
    const token = await Token.deploy("Mock USDT", "USDT", 18);
    await token.waitForDeployment();

    const Delegate = await ethers.getContractFactory("Swappy7702PayoutDelegate");
    const delegate = await Delegate.deploy();
    await delegate.waitForDeployment();

    const tokenAddress = await token.getAddress();
    const delegateAddress = await delegate.getAddress();
    await token.mint(delegateAddress, ethers.parseUnits("1000", 18));

    return { owner, operator, customer, attacker, token, tokenAddress, delegate, delegateAddress };
  }

  it("initializes once with owner and operator", async function () {
    const { owner, operator, delegate } = await deployFixture();

    await expect(delegate.initialize(owner.address, operator.address))
      .to.emit(delegate, "Initialized")
      .withArgs(owner.address, operator.address);

    expect(await delegate.owner()).to.equal(owner.address);
    expect(await delegate.operator()).to.equal(operator.address);
    await expect(delegate.initialize(owner.address, operator.address))
      .to.be.revertedWithCustomError(delegate, "AlreadyInitialized");
  });

  it("allows configured operator payout to configured token and recipient", async function () {
    const { owner, operator, customer, token, tokenAddress, delegate } = await deployFixture();

    await delegate.initialize(owner.address, operator.address);
    await delegate.connect(owner).setTokenAllowed(tokenAddress, true);
    await delegate.connect(owner).setRecipientAllowed(customer.address, true);

    await expect(delegate.connect(operator).payout(ethers.id("buy-contract-1"), tokenAddress, customer.address, ethers.parseUnits("10", 18)))
      .to.emit(delegate, "DelegatePayout");

    expect(await token.balanceOf(customer.address)).to.equal(ethers.parseUnits("10", 18));
  });

  it("blocks contract recipients unless explicitly approved as contracts", async function () {
    const { owner, operator, token, tokenAddress, delegate } = await deployFixture();

    await delegate.initialize(owner.address, operator.address);
    await delegate.connect(owner).setTokenAllowed(tokenAddress, true);
    await delegate.connect(owner).setRecipientAllowed(tokenAddress, true);

    await expect(
      delegate.connect(operator).payout(ethers.id("delegate-contract-blocked"), tokenAddress, tokenAddress, ethers.parseUnits("1", 18))
    ).to.be.revertedWithCustomError(delegate, "ContractRecipientNotAllowed");

    await expect(delegate.connect(owner).setContractRecipientAllowed(tokenAddress, true))
      .to.emit(delegate, "ContractRecipientAllowed")
      .withArgs(tokenAddress, true);

    await delegate.connect(operator).payout(ethers.id("delegate-contract-approved"), tokenAddress, tokenAddress, ethers.parseUnits("1", 18));
    expect(await token.balanceOf(tokenAddress)).to.equal(ethers.parseUnits("1", 18));
  });

  it("rejects adding an EOA to the contract-recipient allowlist", async function () {
    const { owner, customer, delegate } = await deployFixture();

    await delegate.initialize(owner.address, owner.address);

    await expect(delegate.connect(owner).setContractRecipientAllowed(customer.address, true))
      .to.be.revertedWithCustomError(delegate, "ContractRecipientNotAllowed");
  });

  it("blocks unauthorized payout and duplicate operation", async function () {
    const { owner, operator, customer, attacker, tokenAddress, delegate } = await deployFixture();

    await delegate.initialize(owner.address, operator.address);
    await delegate.connect(owner).setTokenAllowed(tokenAddress, true);
    await delegate.connect(owner).setRecipientAllowed(customer.address, true);

    await expect(
      delegate.connect(attacker).payout(ethers.id("bad-actor"), tokenAddress, customer.address, ethers.parseUnits("1", 18))
    ).to.be.revertedWithCustomError(delegate, "Unauthorized");

    const operationId = ethers.id("sell-contract-1");
    await delegate.connect(operator).payout(operationId, tokenAddress, customer.address, ethers.parseUnits("1", 18));
    await expect(
      delegate.connect(operator).payout(operationId, tokenAddress, customer.address, ethers.parseUnits("1", 18))
    ).to.be.revertedWithCustomError(delegate, "OperationAlreadyExecuted");
  });

  it("rejects empty operation id explicitly", async function () {
    const { owner, operator, customer, tokenAddress, delegate } = await deployFixture();

    await delegate.initialize(owner.address, operator.address);
    await delegate.connect(owner).setTokenAllowed(tokenAddress, true);
    await delegate.connect(owner).setRecipientAllowed(customer.address, true);

    await expect(
      delegate.connect(operator).payout(ethers.ZeroHash, tokenAddress, customer.address, ethers.parseUnits("1", 18))
    ).to.be.revertedWithCustomError(delegate, "InvalidOperationId");
  });
});
