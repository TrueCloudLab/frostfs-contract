/*
NeoFSID contract is a contract deployed in NeoFS sidechain.

NeoFSID contract is used to store connection between an OwnerID and its public keys.
OwnerID is a 25-byte N3 wallet address that can be produced from a public key.
It is one-way conversion. In simple cases, NeoFS verifies ownership by checking
signature and relation between a public key and an OwnerID.

In more complex cases, a user can use public keys unrelated to the OwnerID to maintain
secure access to the data. NeoFSID contract stores relation between an OwnerID and
arbitrary public keys. Data owner can bind a public key with its account or unbind it
by invoking Bind or Unbind methods of NeoFS contract in the mainchain. After that,
Alphabet nodes produce multisigned AddKey and RemoveKey invocations of NeoFSID
contract.

# Contract notifications

NeoFSID contract does not produce notifications to process.
*/
package neofsid
