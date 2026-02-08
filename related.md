# Related systems

ACE coordinates agents through content: a producer writes a
structured object into a shared space, and a consumer
retrieves it by pattern without knowing who produced it.
This document describes a real-world system that operates on
the same principle.

## Freight matching

Load boards are a close real-world analog to ACE. A shipper
posts a load with structured attributes: origin, destination,
pickup date, equipment type (dry van, flatbed, refrigerated),
weight, rate. A carrier searches by pattern: available loads
near my current position, matching my truck type, heading
toward my home base. The carrier claims a load, removing it
from the board. Neither party names the other. Coordination
happens through content.

The mapping to ACE operations is direct:

| Freight concept | ACE equivalent |
|-----------------|----------------|
| Post a load | `out` |
| Carrier claims a load | `in` with pattern |
| Carrier browses loads | `rd` with pattern |
| Pickup deadline | TTL |
| Tender with response window | Explicit deletes (visibility timeout) |
| Preferred carrier lists | Access control |
| Equipment type, lane, weight | Matchable fields |
| Special instructions | `#` properties (unmatched data) |

The tender process matches ACE's explicit deletes precisely.
A broker tenders a load to a carrier, who has a window
(typically 15-30 minutes) to accept. If the carrier does not
confirm, the load returns to the board for other carriers.

DAT (originally Dial-A-Truck, founded 1978) is the largest
North American load board. Truckstop is another major
platform. Uber Freight and Amazon Freight are newer entrants.
The spot market, where loads are posted for any qualified
carrier rather than pre-arranged by contract, is the most
tuple-space-like segment: roughly 20% of US truckload volume,
with rates and availability changing continuously.

Carriers optimize for deadhead (empty miles to reach a
pickup) and backhaul (loads heading toward home base). A
carrier finishing a delivery in Memphis wants a load
originating near Memphis going homeward. This is a
multi-attribute pattern match against a large, constantly
changing pool of objects.
