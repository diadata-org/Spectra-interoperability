import { HardhatUserConfig } from "hardhat/config";
import "@nomicfoundation/hardhat-toolbox";


const accounts = process.env.PRIVATE_KEY
  ? [`0x${process.env.PRIVATE_KEY}`]
  : [];

const config: HardhatUserConfig = {
  solidity: "0.8.26",
  networks: {
    hardhat: {
      // accounts: [secret],
      allowUnlimitedContractSize: true,
      gas: 120000000000000,
      blockGasLimit: 0x1fffffffffffff,
    },
    mainnet: {
      url: process.env.MAINNET_RPC || "https://main-light.eth.linkpool.io",
      accounts,
    },
    goerli: {
      url: process.env.GOERLI_RPC || "https://goerli-light.eth.linkpool.io",
      accounts,
    },
    polygon: {
      url: process.env.POLYGON_RPC || "https://polygon-rpc.com",
      accounts,
    },
    mumbai: {
      url: process.env.MUMBAI_RPC || "https://rpc-mumbai.maticvigil.com",
      accounts,
    },
    alfajores: {
      chainId: 44787,
      url: process.env.AKFAJORES_RPC || "https://alfajores-forno.celo-testnet.org",
      accounts,
    },
    sepolia: {
      chainId: 11155111,
      url: process.env.SEPOLIA_RPC || "https://ethereum-sepolia.publicnode.com",
      accounts,
    },
    fuji: {
      chainId: 43113,
      url: process.env.SEPOLIA_RPC || "https://avalanche-fuji.blockpi.network/v1/rpc/public",
      accounts,
    },
    diadata_testnet: {
      chainId: 23104,
      url: process.env.DIADATA__TESTNET_RPC || "https://rpc-static-violet-vicuna-qhcog2uell.t.conduit.xyz",
      accounts,
    },
    bsctestnet: {
      chainId: 97,
      url: process.env.BSC__TESTNET_RPC || "https://bsc-testnet-rpc.publicnode.com",
      accounts,
    },
    base_sepolia: {
        chainId: 84532,
        url: process.env.BASE_SEPOLIA__TESTNET_RPC || "https://base-sepolia-rpc.publicnode.com",
        accounts,
    },
    arbi_sepolia: {
      chainId: 421614,
      url: process.env.ARBI_SEPOLIA__TESTNET_RPC || "https://arbitrum-sepolia-rpc.publicnode.com",
      accounts,      
    },
    op_sepolia: {
      chainId: 11155420,
      url: process.env.OPTIMISM_SEPOLIA__TESTNET_RPC || "https://sepolia.optimism.io",
      accounts,      
    },
  },
};

export default config;