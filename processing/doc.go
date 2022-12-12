/*
Processing contract is a contract deployed in NeoFS mainchain.

Processing contract pays for all multisignature transaction executions when notary
service is enabled in the mainchain. Notary service prepares multisigned transactions,
however they should contain sidechain GAS to be executed. It is inconvenient to
ask Alphabet nodes to pay for these transactions: nodes can change over time,
some nodes will spend sidechain GAS faster. It leads to economic instability.

Processing contract exists to solve this issue. At the Withdraw invocation of
NeoFS contract, a user pays fee directly to this contract. This fee is used to
pay for Cheque invocation of NeoFS contract that returns mainchain GAS back
to the user. The address of the Processing contract is used as the first signer in
the multisignature transaction. Therefore, NeoVM executes Verify method of the
contract and if invocation is verified, Processing contract pays for the
execution.

# Contract notifications

Processing contract does not produce notifications to process.
*/
package processing
