/*
Netmap contract is a contract deployed in NeoFS sidechain.

Netmap contract stores and manages NeoFS network map, Storage node candidates
and epoch number counter. In notary disabled environment, contract also stores
a list of Inner Ring node keys.

# Contract notifications

AddPeer notification. This notification is produced when a Storage node sends
a bootstrap request by invoking AddPeer method.

	AddPeer
	  - name: nodeInfo
	    type: ByteArray

UpdateState notification. This notification is produced when a Storage node wants
to change its state (go offline) by invoking UpdateState method. Supported
states: (2) -- offline.

	UpdateState
	  - name: state
	    type: Integer
	  - name: publicKey
	    type: PublicKey

NewEpoch notification. This notification is produced when a new epoch is applied
in the network by invoking NewEpoch method.

	NewEpoch
	  - name: epoch
	    type: Integer
*/
package netmap
