
//0x6D41241dDc38F8552eAbC5b9875b3cC521f090d3


import { ethers } from "hardhat";

async function main() {
  const OracleUpdateRecipient = await ethers.getContractFactory("OracleRequestRecipient");
 

  const oc = await OracleUpdateRecipient.attach("0xF4600F77C0ca6Ade3d5374030B1B7655B821ceBd")
 
//   const receivedData = await oc.receivedData();

let abicoder = new ethers.AbiCoder()
let encoded = abicoder.encode([ "string","uint128", "uint128" ], [  "BTC/USD",1234,1234 ]);

// encoded = "0x00000000000000000000000000000000000000000000000000000000000000600000000000000000000000000000000000000000000000000000000066b76a16000000000000000000000000000000000000000000000000000005852a8232a000000000000000000000000000000000000000000000000000000000000000074254432f55534400000000000000000000000000000000000000000000000000"

// console.log("encoded",encoded)





const rd = await oc.lastData()

let result = abicoder.decode([ "uint256", "uint256", "uint256" ],rd)

console.log("result",result)
  console.log("Received Data:", rd);

  // const update = await oc.updates("WETH/USD")

  // console.log(update)


}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
