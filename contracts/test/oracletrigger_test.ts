// test/OracleTrigger.test.js
const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("OracleTrigger", function () {
  let OracleTrigger;
  let oracleTrigger:any;
  let owner:any;
  let addr1:any;
  let addr2:any;

  beforeEach(async function () {
    OracleTrigger = await ethers.getContractFactory("OracleTrigger");
    [owner, addr1, addr2] = await ethers.getSigners();
    oracleTrigger = await OracleTrigger.deploy();
    await oracleTrigger.deployed();
  });

  describe("Chain Management", function () {
    it("Should add a new chain configuration", async function () {
      const chainId = 1;
      const mailboxAddress = addr1.address;
      const recipientAddress = addr2.address;

      await oracleTrigger.addChain(chainId, mailboxAddress, recipientAddress);

      const chainConfig = await oracleTrigger.viewChain(chainId);
      expect(chainConfig[0]).to.equal(mailboxAddress);
      expect(chainConfig[1]).to.equal(recipientAddress);
    });

    it("Should fail to add a chain if it already exists", async function () {
      const chainId = 1;
      const mailboxAddress = addr1.address;
      const recipientAddress = addr2.address;

      await oracleTrigger.addChain(chainId, mailboxAddress, recipientAddress);

      await expect(
        oracleTrigger.addChain(chainId, mailboxAddress, recipientAddress)
      ).to.be.revertedWith("Chain ID already exists");
    });

    it("Should update an existing chain configuration", async function () {
      const chainId = 1;
      const mailboxAddress = addr1.address;
      const recipientAddress = addr2.address;

      await oracleTrigger.addChain(chainId, mailboxAddress, recipientAddress);

      const newMailboxAddress = owner.address;
      const newRecipientAddress = addr1.address;

      await oracleTrigger.updateChain(chainId, newMailboxAddress, newRecipientAddress);

      const chainConfig = await oracleTrigger.viewChain(chainId);
      expect(chainConfig[0]).to.equal(newMailboxAddress);
      expect(chainConfig[1]).to.equal(newRecipientAddress);
    });

    it("Should fail to update a non-existent chain", async function () {
      const chainId = 1;
      const newMailboxAddress = owner.address;
      const newRecipientAddress = addr1.address;

      await expect(
        oracleTrigger.updateChain(chainId, newMailboxAddress, newRecipientAddress)
      ).to.be.revertedWith("Chain ID does not exist");
    });

    it("Should return the correct chain configuration", async function () {
      const chainId = 1;
      const mailboxAddress = addr1.address;
      const recipientAddress = addr2.address;

      await oracleTrigger.addChain(chainId, mailboxAddress, recipientAddress);

      const chainConfig = await oracleTrigger.viewChain(chainId);
      expect(chainConfig[0]).to.equal(mailboxAddress);
      expect(chainConfig[1]).to.equal(recipientAddress);
    });

    it("Should fail to return a non-existent chain configuration", async function () {
      const chainId = 1;

      await expect(oracleTrigger.viewChain(chainId)).to.be.revertedWith("Chain ID does not exist");
    });
  });

  describe("Dispatch Functions", function () {
    // You would need to mock the IMailbox and IDIAOracleV2 contracts for these tests.
  });

  describe("Handle Function", function () {
    it("Should emit Handled event if block hash does not end in 0", async function () {
      const blockHash = await oracleTrigger.previousBlockHash();
      const isBlockHashEndIn0 = BigInt(blockHash) % 16n === 0n;

      if (!isBlockHashEndIn0) {
        await expect(oracleTrigger.handle(1, blockHash, "0x"))
          .to.emit(oracleTrigger, "Handled")
          .withArgs(blockHash);
      } else {
        await expect(oracleTrigger.handle(1, blockHash, "0x"))
          .to.be.revertedWith("block hash ends in 0");
      }
    });
  });
});