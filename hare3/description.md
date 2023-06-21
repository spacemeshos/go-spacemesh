# Message handling

Goals:
 * Ensure that the network cannot be flooded with equivocating messages.
 * Ensure participants agree at each round on the set of malfeasant
   participants, so that they can correctly calculate thresholds using the `>=
   f+1-k` formula where `f` is the limit of tolerable failures and `k` is the
   number of known malfeasant participants.
 * Try to keep the code as simple as possible
 * Handle early messages as much as possible on receipt to avoid a build up
   that could cause a bottleneck when starting the next layer.

Preventing flooding by equivocation could be handled easily by not relaying any
messages from identities for which we already have a message for the round. The
problem with this approach is that if none of the equivocating messages are
relayed, other participants will not be able to detect the equivocation and this
will result in differing threshold calculations. The node that detected the
equivocation will have incremented its `k` value and those that did not detect
the equivocation will not.

So instead to ensure that the network has a consistent view of equivocating
identities we need to relay the first message that allowed for equivocation to
be detected but no further equivocating messages. So the rules for relaying
should be that all good (non equivocating) messages should be relayed and the
first message that resulted in detection of equivocation for an identity in a
round should be relayed. 

There are 2 distinct ways to discover malfeasant identities in a round:
1. An identity sends more than one equivocating message in that round.
1. An identity for which we have a stored malfeasance proof sends a message in that round.


The hare3 protocol is built such that messages are not acted upon immediately,
but instead after some delay, which due to the gossip guarantee (message
delivery times can differ between nodes by up to one round), ensures that if
any node discovered a message to be malfeasant all nodes will consider that
message malfeasant.

This frees us from needing to process messages in any specific order. Instead
messages are accumulated into the three sub protocols(GradedGossip, Gradecast &
ThesholdGossip) which are later inspected by the main protocol, at a point such
that all parties will share the same view.

To simplify the implementation it is assumed that all nodes share the same view
of malfeasance proofs at any given layer (this is currently not the case but
there is a ticket to resolve this
https://github.com/spacemeshos/go-spacemesh/issues/3987) which means that
fowarding a message from a known malfeasant identity is equivalent to
forwarding a malfeasance proof, since the receiver will already have the
malfeasance proof. This assumption allows us to handle just one type of message
(the hare message) and have one path for handling messages. This could be
modified in the future but for now, simplifying in this way should allow us to
get to a working version faster. 

Early messages are handled on receipt and relayed if they look to be correct.
This can result in sending messages that would not have been sent if the early
message was handled after its layer had started, this would happen if we see
equivocating early messages and equivocation for the sending identity will
later be detected in the previous layer, and so had we waited there would have
been a stored equivocation proof for that identity. The overall effect of this
will be minimal, the difference being that instead of relaying one message for
that identity we will have sent two.

In order to ensure a consistent view of the network we will need to re-handle
the early messages, to account for malfeasance proofs that could have been
discovered in the previous layer after they early message was processed.

# Message handling components

## hare3.Broker

Receives messages from the network validated them and passes them to the hare
handler. Indicates to the network if messages should be relayed.

* hare.Broker.HandleMessage:
  * Accepts messages from p2p pubsub, the return value indicates whether pubsub
	should relay the message or not.
  * Performs validation on messages and routes them to the appropriate hare
	handler, or creates the handler if the message is early. If validation
	fails or the message is not early and no handler has been registered then
	the message is dropped.
  * Checks the mesh to see if the message sender is malfeasant.
  * Calls hare3.Handler.HandleMsg with the message and an indication of
	whether it came from a malfeasant identity. The hare handler returns an
	indication of whether the message should be relayed and also an
	equivocating message if the received message resulted in a new
	equivocation.
  * If the message should be relayed store the raw message, so that if an
	equivocating message is discovered we can build a malfeasance proof. The
	store of messages is also used for early messages when they are re-handled
	at the beginning of their layer.

## hare3.Handler

Receives messages from the broker, accumulates them in its three sub protocols
(GradedGossip, Gradecast & ThesholdGossip), indicates whether it detected
equivocation and if the message should be relayed.

* hare3.Handler.HandleMsg:
  * Accepts messages from the broker, the return value inidcates whether pubsub
	should relay the message or not and also whether equivocation was detected.
  * Forwards incoming messages to 3 sub protocols. GradedGossip, Gradecast and
	ThresholdGradedGossip.
  * First the message is passed to GradedGossip the output of which determines
	whether the message should be dropped, whether equivocation was detected
	and also a set of values to be passed to either Gradecast or
	ThresholdGradedGossip.
  * If the message is not to be dropped the output set of values are passed to
	either Gradecast or ThresholdGradedGossip depending on the type of the
	message.
  * Returns an indication of whether the message should be relayed, and an
	equivocating message if detected.

## hare3.GradedGossip

Accumulates messages, outputting an updated set of values for the identity and
round defined in each received message. This updated set of values can change
from a valid set of values to the empty set in the case that equivocation was
detected. Also acts as a filter by dropping messages from a previously detected
malfeasant identity.

* hare3.GradedGossip.ReceiveMsg

## hare3.Gradecast

Accumulates output from GradedGossip, and provides a means for it's content to
be inspected after some delay such that all parties will share a view of either
a valid message or an instance of malfeasance.

## hare3.ThresholdGradedGossip

Accumulates output from GradedGossip, and maintains vote counts for all
distinct values, provides a means for it's content to Be inspected after some
delay such that all parties will share a view of the values that have passed
the threshold.

# Protocol execution

The protocol is driven through a series of rounds in response to events from a
clock, at each execution of a round the protocol may return a message that
should be published to the network and an output, which if non nil indicates
that the protocol has terminated. At each step the protocol inspects Gradecast
and ThresholdGradedGossip to see if some state has been reached, if so the
protocol updates its internal state.

The hare3.Protocol and the hare3.Handler share a mutex which serializes their
access to the sub protocols that are accumulating messages.

## hare3.Runner

A simple wrapper around a protocol instance, a clock and a component to publish messages to the network.

* hare3.Runner.Run:
  * Awaits new round events from the clock
  * Calls Protocol.NextRound and broadcasts any message returned to the network.
  * If the protocol returns a result, Run returns it.
  * If the protocol exceeds some max number of rounds Run returns an empty result.

## hare3.Protocol

The protocol is constructed with GradedGossip and Gradecast components which
are updated asynchronously by the hare3.Handler. It has one method NextRound
that is called repeatedly until the protocol terminates and returns a value.
NextRound can also return messages to be broadcast to the network. Internally
NextRound inspects the state of the GradedGossip and Gradecast components and
determines what action to take.
