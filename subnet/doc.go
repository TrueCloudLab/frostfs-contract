/*
Subnet contract is a contract deployed in NeoFS sidechain.

Subnet contract stores and manages NeoFS subnetwork states. It allows registering
and deleting subnetworks, limiting access to them, and defining a list of the Storage
Nodes that can be included in them.

# Contract notifications

Put notification. This notification is produced when a new subnetwork is
registered by invoking Put method.

	Put
	  - name: id
	    type: ByteArray
	  - name: ownerKey
	    type: PublicKey
	  - name: info
	    type: ByteArray

Delete notification. This notification is produced when some subnetwork is
deleted by invoking Delete method.

	Delete
	  - name: id
	    type: ByteArray

RemoveNode notification. This notification is produced when some node is deleted
by invoking RemoveNode method.

	RemoveNode
	  - name: subnetID
	    type: ByteArray
	  - name: node
	    type: PublicKey
*/
package subnet
