/*
Proxy contract is a contract deployed in NeoFS sidechain.

Proxy contract pays for all multisignature transaction executions when notary
service is enabled in the sidechain. Notary service prepares multisigned transactions,
however they should contain sidechain GAS to be executed. It is inconvenient to
ask Alphabet nodes to pay for these transactions: nodes can change over time,
some nodes will spend sidechain GAS faster. It leads to economic instability.

Proxy contract exists to solve this issue. While Alphabet contracts hold all
sidechain NEO, proxy contract holds most of the sidechain GAS. Alphabet
contracts emit half of the available GAS to the proxy contract. The address of the
Proxy contract is used as the first signer in a multisignature transaction.
Therefore, NeoVM executes Verify method of the contract; and if invocation is
verified, Proxy contract pays for the execution.

# Contract notifications

Proxy contract does not produce notifications to process.
*/
package proxy
