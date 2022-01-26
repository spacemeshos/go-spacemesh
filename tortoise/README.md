Tortoise is used to make decisions on the blocks that will be applied to the ledger. The protocol is asynchronous and makes progress based on the received data, without timing assumptions.

## Core concepts

#### Block

From the tortoise perspective it is important that the block has a unique ID and belongs to some layer.

In the rest of the system block is a wrapper for transactions and rewards. So once tortoise decides on a block - related transactions and rewards will be applied to the ledger.

#### Ballot

Tortoise counts votes from the syntactically valid ballots.

For tortoise purposes every ballot must have a unique ID, reference to the base ballot ID, explicit votes (exceptions), weight and a beacon.

##### Base ballot and exceptions

Discussed in [Voting section](#voting).

##### Weight

Weight of the ballot is computed from the weight of the activation transaction divided by the total number of eligible ballots. Total number of eligible ballots is computed based on the fraction of activation transaction compared to the weight from the active set that was recorded in reference ballot.

##### Beacon

Beacon is a source of randomness and is calculated by the all smeshers per epoch. Ballots with a different beacon is either malicious or a result of a network split.

Ballot with an inconsistent beacon will not be marked good and will not be counted immediately. Discussed in [full](#votes-from-ballots-with-wrong-beacon) and [verifying](#good-ballots) sections.

#### Expected weight and thresholds

Expected weight is a total weight from the activation transactions. Unlike weight for the ballot we are using a sum of all activation transactions that target a specific epoch. Expected weight for the layer is fraction of the expected weight for the epoch.

## Voting

When voting protocol chooses base ballot and encodes the difference between local opinion and base ballot votes. Since we don't keep votes from genesis in memory the implementation has a shortcoming that it encodes difference only within some configurable window (we can refer to it as voting window). 

In order to minimize amount of data that needs to be sent and stored, Tortoise selects ballot that has the least difference with a local opinion. 

Types of votes:
- support

    Support all blocks according to the local opinion that are not supported by base ballot. Usually support is added for layers starting from base ballot layer up to the last processed layer. In the event of healing tortoise may add support for blocks before base ballot layer.

- against
  
    Against is added only if the base ballot supports the block and local opinion is against that block. 

- abstain (undecided) 
    
    Undecided is a valid vote only until hare is terminated for the layer (it may take more than one layer for hare to terminate, this is expressed as zdist).

If ballot doesn't specify explicit vote for a block then it votes against the block. This prevents malicious smeshers from retroactively reorganizing mesh by intentionally creating late ballots.

There are 4 distinct reasons to cast a vote:
- hare output
    
    tortoise uses output from the hare consensus for voting on blocks within hdist.

- validity

    for layer before hdist the vote is cast according to the decision. 

- local threshold

    in case when decision wasn't reached but margin from counted weight crosses local threshold tortoise will cast a vote according to the local threshold.

- weak coin

    otherwise tortoise votes according to the coin recorded in the last layer. if sufficient number of honest votes were cast this way then tortoise will either reach decision or atleast vote according to the local threshold in future layers. if honest nodes disagreed on the coin value then the voting will continue until they agree.

## Full tortoise

Full tortoise counts votes from every ballot on every block before the ballot. Once counted weight cross global threshold tortoise can make a decision on a block. Layer is finalized only when all blocks have a decision.

| ballot/block | weight | 0x11 | 0x22 | 0x33 | 0x44 |
| ------------ | ------ | ---- | ---- | ---- | ---- |
| 0xaa         | 10     | 1    | -1   | -1   | 1    |
| 0xbb         | 20     | 1    | 1    | -1   | 1    |
| total        | -      | 30   | 10   | -30  | 30   |
| threshold    | -      | 20   | 20   | 20   | 20   |
| decision     | -      | 1    | 0    | -1   | 1    |

In the above example there are two ballots (0xaa and 0xbb). They vote for 4 blocks. Blocks 0x11 and 0x44 are valid and can be applied to the ledger, block 0x33 is invalid, and block 0x22 is still undecided.

#### Votes from ballots with wrong beacon

Because a smesher can select arbitrary beacon, there is an attack that will allow malicious smeshers to concentrate weight in a specific layer. If tortoise counts malicious weight immediately it will make erroneous decisions (decide that a block is valid when in fact it is not). In order to prevent such attack tortoise will delay counting ballots with a wrong beacon until enough weight from honest smeshers was counted. 

Note that we can't discard ballots with wrong beacon completely as recovery from partition requires counting ballots from both sides. And they will likely have different beacons if it was a long partition, or occurred at the time when beacon was computed.

#### Scaling issues

Full tortoise complexity grows with the number of layers that are not finalized. 

During live-tortoise this can be a problem only in extreme cases when recent layer fails to be finalized. Due to the excessive malicious voting (> 2/3 fraction of total weight) or insufficient voting from honest smeshers (due to the network problems).

However, during so called rerun (or sync) tortoise needs to count votes from all layers. Otherwise there is always a risk that votes from latest layers changed decision that was made without counting them. With optimized full tortoise it might be practical to count votes when there are 2000 undecided layers, but not higher.

In practice during rerun layer is expected to be finalized after tortoise counted votes from some limited number of layers, this is referred to as verification window in the codebase.  

## Verifying tortoise

Verifying tortoise is meant as a solution for [scaling-issues](#scaling-issues) of the full tortoise.

Intuitively if there is enough weight from ballots that vote consistently with each other to finalize a layer then tortoise doesn't full vote counting and can use more efficient batched method.

#### Good ballots

Each node maintains a table with local opinions on blocks. For a layer within hdist the opinion is based on the hare output. On layers outside hdist the opinion is based on the decisions from counted votes. Simple example:

| block | layer | opinion |
| ----- | ----- | ------- |
| 0x22  | 11    | -1      |
| 0x33  | 12    | 1       |
| 0x44  | 13    | 1       |

Important detail that abstain vote is consistent with any local opinion.Otherwise even a single abstain vote will force to switch tortoise into full mode.     

Whenever tortoise receives a ballot it checks if votes from this ballot are consistent with local table:

| block/ballot | layer | opinion | 0xaa | 0xbb |
| ------------ | ----- | ------- | ---- | ---- |
| 0x11         | 10    | 1       | 1    | 1    |
| 0x22         | 11    | -1      | 1    | -1   |
| 0x33         | 12    | 1       | 1    | 1    |
| 0x44         | 13    | 1       | -1   | 1    |

In this example only 0xbb ballot is consistent with local opinion. Consistency with a local opinion is a part of the definition of the good ballot, full definition is:
- ballot has a correct beacon
- ballot doesn't vote for block before base ballot layer
- ballot votes are consistent with local opinion 
- base ballot is good

In the implementation we have a state named "can be good", the role of it's state will be clarified in [modes interaction](#modes-interaction) section. Ballot is marked "can be good" if first 3 conditions are satisfied, but not necessarily the last one.

#### Vote counting in verifying mode

Votes in verifying mode are counted in a batched form, unlike full tortoise. After "goodness" of the ballot is determinted tortoise filters good ballots and sums up their weight. The result is a counted weight that is consistent with our local opinion. To get an accurate margin that can be compared with global threshold we assume that all uncounted ballots votes against our local opinion.

## Modes interaction

Verifying mode is safer as tortoise doesn't have an imposed limit on the amount of counted votes (or at least imposed limit can be signicantly higher). Naturally we want to stay in verifying mode as long as tortoise can make progress.

#### When to switch into full mode?

Tortoise certainly has to switch into full mode if there is a layer that wasn't finalized past hdist layers. In such case it needs to count previously uncounted ballots so that real margin can be compared with local threshold, see [voting](#voting). Without counting all votes we can get only pessimistic margin, which works for the purposes of the verifying tortoise, but may not work for self-healing purposes (e.g. instead of voting according to local threshold sign tortoise will vote according to weak coin).

During rerun we expect verifying tortoise to make progress after counting `verifying-mode-verification-window` layers. This window can be as large as memory will allow us, computational complexity doesn't increase with the window. However if tortoise didn't make progress after counting votes within this window it will have to switch into full mode during rerun. 

#### When to switch into verifying mode?

It is safe to make an attempt at any point. However we need a sufficient number of ballots that vote consistently with each other in order for verifying tortoise to work. 

In the implementation, we make an attempt after full tortoise made progress and changed local opinion on layers that were undecided according to the verifying tortoise. In the layer that was just finalized by full tortoise we find ballots that ["can be good"](#good-ballots) with base ballots in the same state. After they are marked good we restart verifying tortoise and if same layer it made the same progress as full tortoise we switch the mode.

| block/ballot | local opinion | 0xaa | 0xbb | 0xcc | 0xdd | 0xee | 0xff |
| ------------ | ------------- | ---- | ---- | ---- | ---- | ---- | ---- |
| base         | -             | -    | -    | 0xaa | 0xaa | 0xcc | 0xcc |
| layer        | -             | 10   | 10   | 11   | 11   | 12   | 12   |
| 0x11         | 1             | -1   | -1   | 1    | -1   | 1    | 1    |
| 0x22         | 1             | -1   | -1   | 1    | -1   | 1    | 1    |
| 0x33         | -1            | -    | -    | -1   | -1   | -1   | -1   |
| 0x44         | -1            | -    | -    | -1   | -1   | -1   | -1   |
| 0x55         | 1             | -    | -    | -    | -    | 1    | 1    |
| 0x66         | -1            | -    | -    | -    | -    | -1   | -1   |

In this example ballots starting from layer 10 are voting for some old layer (for example 9). Ballots from layer 10 disagree with our local opinion and will be marked bad. Ballot 0xcc from layer 10 and ballots 0xee and 0xff will be marked "can be good" because exceptions from those ballots are consistent with local opinion.