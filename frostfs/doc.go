/*
FrostFS contract is a contract deployed in FrostFS mainchain.

FrostFS contract is an entry point to FrostFS users. This contract stores all FrostFS
related GAS, registers new Inner Ring candidates and produces notifications
to control the sidechain.

While mainchain committee controls the list of Alphabet nodes in native
RoleManagement contract, FrostFS can't change more than 1\3 keys at a time.
FrostFS contract contains the actual list of Alphabet nodes in the sidechain.

Network configuration is also stored in FrostFS contract. All changes in
configuration are mirrored in the sidechain with notifications.

# Contract notifications

Deposit notification. This notification is produced when user transfers native
GAS to the FrostFS contract address. The same amount of FROSTFS token will be
minted in Balance contract in the sidechain.

	Deposit:
	  - name: from
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: receiver
	    type: Hash160
	  - name: txHash
	    type: Hash256

Withdraw notification. This notification is produced when a user wants to
withdraw GAS from the internal FrostFS balance and has paid fee for that.

	Withdraw:
	  - name: user
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: txHash
	    type: Hash256

Cheque notification. This notification is produced when FrostFS contract
has successfully transferred assets back to the user after withdraw.

	Cheque:
	  - name: id
	    type: ByteArray
	  - name: user
	    type: Hash160
	  - name: amount
	    type: Integer
	  - name: lockAccount
	    type: ByteArray

Bind notification. This notification is produced when a user wants to bind
public keys with the user account (OwnerID). Keys argument is an array of ByteArray.

	Bind:
	  - name: user
	    type: ByteArray
	  - name: keys
	    type: Array

Unbind notification. This notification is produced when a user wants to unbind
public keys with the user account (OwnerID). Keys argument is an array of ByteArray.

	Unbind:
	  - name: user
	    type: ByteArray
	  - name: keys
	    type: Array

AlphabetUpdate notification. This notification is produced when Alphabet nodes
have updated their lists in the contract. Alphabet argument is an array of ByteArray. It
contains public keys of new alphabet nodes.

	AlphabetUpdate:
	  - name: id
	    type: ByteArray
	  - name: alphabet
	    type: Array

SetConfig notification. This notification is produced when Alphabet nodes update
FrostFS network configuration value.

	SetConfig
	  - name: id
	    type: ByteArray
	  - name: key
	    type: ByteArray
	  - name: value
	    type: ByteArray
*/
package frostfs
