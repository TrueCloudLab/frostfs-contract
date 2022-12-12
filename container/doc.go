/*
Container contract is a contract deployed in NeoFS sidechain.

Container contract stores and manages containers, extended ACLs and container
size estimations. Contract does not perform sanity or signature checks of
containers or extended ACLs, it is done by Alphabet nodes of the Inner Ring.
Alphabet nodes approve it by invoking the same Put or SetEACL methods with
the same arguments.

# Contract notifications

containerPut notification. This notification is produced when a user wants to
create a new container. Alphabet nodes of the Inner Ring catch the notification and
validate container data, signature and token if present.

	containerPut:
	  - name: container
	    type: ByteArray
	  - name: signature
	    type: Signature
	  - name: publicKey
	    type: PublicKey
	  - name: token
	    type: ByteArray

containerDelete notification. This notification is produced when a container owner
wants to delete a container. Alphabet nodes of the Inner Ring catch the notification
and validate container ownership, signature and token if present.

	containerDelete:
	  - name: containerID
	    type: ByteArray
	  - name: signature
	    type: Signature
	  - name: token
	    type: ByteArray

setEACL notification. This notification is produced when a container owner wants
to update an extended ACL of a container. Alphabet nodes of the Inner Ring catch
the notification and validate container ownership, signature and token if
present.

	setEACL:
	  - name: eACL
	    type: ByteArray
	  - name: signature
	    type: Signature
	  - name: publicKey
	    type: PublicKey
	  - name: token
	    type: ByteArray

StartEstimation notification. This notification is produced when Storage nodes
should exchange estimation values of container sizes among other Storage nodes.

	StartEstimation:
	  - name: epoch
	    type: Integer

StopEstimation notification. This notification is produced when Storage nodes
should calculate average container size based on received estimations and store
it in Container contract.

	StopEstimation:
	  - name: epoch
	    type: Integer
*/
package container
