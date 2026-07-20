require("@nomicfoundation/hardhat-toolbox");
const { firstEnv, firstUrlEnv, loadContractEnv, normalizePrivateKey } = require("./scripts/env");

loadContractEnv();

const privateKey = normalizePrivateKey(firstEnv("DEPLOYER_PRIVATE_KEY", "PRIVATE_KEY", "EVM_PRIVATE_KEY"));
const bscRpc = firstUrlEnv("BSC_RPC_URL", "BSC_RPC_URLS", "RPC_URL");
const bscTestnetRpc = firstUrlEnv("BSC_TESTNET_RPC_URL", "BSC_TESTNET_RPC_URLS");
const polygonRpc = firstUrlEnv("POLYGON_RPC_URL", "POLYGON_RPC_URLS");
const polygonAmoyRpc = firstUrlEnv("POLYGON_AMOY_RPC_URL", "POLYGON_TESTNET_RPC_URL", "POLYGON_AMOY_RPC_URLS");

const accounts = privateKey ? [privateKey] : [];

module.exports = {
  paths: {
    sources: "./src",
    tests: "./test",
    cache: "./cache",
    artifacts: "./artifacts"
  },
  solidity: {
    version: "0.8.24",
    settings: {
      optimizer: {
        enabled: true,
        runs: 200
      }
    }
  },
  networks: {
    hardhat: {
      chainId: 31337
    },
    bsc: {
      url: bscRpc || "https://bsc-dataseed.binance.org/",
      chainId: 56,
      accounts
    },
    bscTestnet: {
      url: bscTestnetRpc || "https://data-seed-prebsc-1-s1.binance.org:8545/",
      chainId: 97,
      accounts
    },
    polygon: {
      url: polygonRpc || "https://polygon-rpc.com/",
      chainId: 137,
      accounts
    },
    polygonAmoy: {
      url: polygonAmoyRpc || "https://polygon-amoy-bor-rpc.publicnode.com",
      chainId: 80002,
      accounts
    }
  }
};
