// Compute keccak256 topic0 for a list of Solidity event signatures.
//
// Reads a JSON map {"EventName": "EventName(type1,type2,...)"} from stdin (or
// first positional arg as file path) and prints {"EventName": "0x..."}.
//
// Used by CI to keep packages/shared/src/events/{ydToken,certificateNft}.ts
// placeholders (Minted / CapSet / CertificateMinted) in sync with the actual
// Solidity source. Currently only the standard OZ events + the computed ones
// are emitted; the shared package imports them as constants.
//
// Usage:
//   node ./script/compute-topics.mjs events.json
//   echo '{"Transfer":"Transfer(address,address,uint256)"}' | node ./script/compute-topics.mjs

import {readFileSync} from "node:fs";
import {resolve} from "node:path";

function keccak256(input) {
    // Use viem (already a dep in apps/web) for proper Ethereum keccak.
    // We import dynamically so this script can run without bundlers.
    // Fallback: Node 22 has built-in `crypto.subtle` but no keccak;
    // so we depend on viem being resolvable from the script dir.
    // eslint-disable-next-line @typescript-eslint/no-var-requires
    const {keccak256: k, stringToBytes} = require("viem");
    return k(stringToBytes(input));
}

async function main() {
    const arg = process.argv[2];
    let input;
    if (arg) {
        input = JSON.parse(readFileSync(resolve(arg), "utf8"));
    } else {
        const chunks = [];
        for await (const chunk of process.stdin) chunks.push(chunk);
        input = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    }

    const out = {};
    for (const [name, sig] of Object.entries(input)) {
        out[name] = keccak256(sig);
    }
    process.stdout.write(JSON.stringify(out, null, 2) + "\n");
}

main().catch((err) => {
    console.error(err);
    process.exit(1);
});