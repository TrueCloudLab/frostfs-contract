/*
Reputation contract is a contract deployed in NeoFS sidechain.

Inner Ring nodes produce data audit for each container during each epoch. In the end,
nodes produce DataAuditResult structure that contains information about audit
progress. Reputation contract provides storage for such structures and simple
interface to iterate over available DataAuditResults on specified epoch.

During settlement process, Alphabet nodes fetch all DataAuditResult structures
from the epoch and execute balance transfers from data owners to Storage and
Inner Ring nodes if data audit succeeds.

# Contract notifications

Reputation contract does not produce notifications to process.
*/
package reputation
