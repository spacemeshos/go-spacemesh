## p2p Routing
- Each node has hard-coded list of at least one but most likely up to 5 bootstrap nodes. One bootstrap node must be available for new nodes.
- when a node starts up, it uses khd routing protocol to add itself to the bootstrap node. 
- This will return a list of nodes that are 'close' (under xor arithmetic) to the node.
These are effectively random nodes as node ids are random.
- A node should most likely only keep up to n=5 online neighbors from the close peers list at any time and have additional peers on stand-by in case it fails to connect to one of the neighbors.
- Similar ideas found in libp2p and eth peer routing algos.
- As the bootstrap node will add the new node to its routing table, other nodes will try to connect to the new node as it will be close to them at random.
- So joining the network requires only 1 call searchPeer(self) and 1 online bootstrap node.


## p2p Networking requirements

- Node Identity - Node id is derived from its public key.
- Encryption - end-to-end with forward-security (sym enc keys negotiated per session between nodes)
- Authentication - Receiver node should be able to auth sender node id
- Node Discovery - DHT: random discovery of neighbors for gossip protocol. Node should be able to dial any online node and connect to it.
- Bootstrapping - hard-coded nodes-set - start neighbors walk from this list.
- Base Protocol and multi-protocols - Support app-level protocols framed w base protocol.
- Protocols Multiplexing - Support multi-protocol messages over the same virtual connection.
- Protocols versioning - Support negotiation of protocol version between nodes.
- Gossip Protocol - Allow nodes to gossip over the base protocol

- RPC: make it easy to add versioned services. For example: build something like this https://github.com/beingmohit/libp2p-rpc/blob/master/index.js on top of libp2p in go.

### Wire protocol (serialization)
We are going with `protobufs` as wire format for all data instead of having to write our own wire format (such as eth hand-rolled RLP format). There's great go support for this format.

### Working with User Accounts
- Basic stuff for working with accounts (create, review, unlock)
- Unlock account method - caller must provide account pass-phrase to an account that is in the node's local keystore file. Session will unlock local account for signing purposes. All methods that require account signature can sign on behalf of that account.
- Methods that require account signature will have a version that supports accepting an account signature on the data. This allows nodes to execute such methods without access to the account private key.

## p2p protocol support - design
Ideally we'll build a `protobufs` based RPC capabilities over `lib-p2p` for p2p protocols. Here's how it this can work.

- lib-p2p supports a prtobufs multi-codec (for sending and receiving protobufs over a connection and for decoding / encoding protobufs for wire transfers (addition of the binary payload length prefix)). https://github.com/multiformats/go-multicodec/ and multi-protocols per host.

- Protocols can be defined using `proto3` files and the `message` syntax.

- lib-p2p own KHD routing protocol is an example of an implementation of a protocol in GO that is using `protobufs` for the message binary format although 1 message type is used for all methods (not very type-safe and fragile). See: https://github.com/libp2p/go-libp2p-kad-dht

#### Multi-protocols support design

#### Defining a protocol using protobufs

Each request and response are specified as a `lib-p2p` `multi-protocol`. Protocol impl registers handlers for these messages on the lib-p2p `host`. This design makes it unnecessary to build an `RPC-like` dispatch while keeping the RPC concept - messages initiated by a client and remote clients responding to remote initiated messages. As an example, a `Ping` protocol includes 1 ping method which is defined of 2 type of messages - `PingRequest` and `PingResponse`.  

```proto
syntax = "proto3";

package protocols.p2p;

// designed to be shared between all app protocols
message MessageData {
    // shared between all requests
    string clientVersion = 1; // client version
    int64 timestamp = 2;     // unix time
    string id = 3;           // allows requesters to use request data when processing a response
    bool gossip = 4;         // true to have receiver peer gossip the message to neighbors
    string nodeId = 5;       // id of node that created the message (not the peer that may have sent it). =base58(mh(sha256(nodePubKey)))
    bytes nodePubKey = 6;    // Authoring node Secp256k1 public key (32bytes)
    string sign = 7;         // signature of message data + method specific data by message authoring node
}

//// ping protocol

// a protocol define a set of reuqest and responses
message PingRequest {
    MessageData messageData = 1;

    // method specific data
    string message = 2;
    // add any data here....
}

message PingResponse {
    MessageData messageData = 1;

    // response specific data
    string message = 2;
    // ... add any data here
}

//// echo protocol

// a protocol define a set of reuqest and responses
message EchoRequest {
    MessageData messageData = 1;

    // method specific data
    string message = 2;
    // add any data here....
}

message EchoResponse {
    MessageData messageData = 1;

    // response specific data
    string message = 2;
    // ... add any data here
}
```

##### Handling incoming remote requests (generating responses)

1. Specify protocol messages - a request and response for each method (protobufs)
2. For each message, specify request data and response data objects (protobufs)
3. For each defined message, implement a method (callback) to handle remote method request (or response). Method will get marshaled data and will create a response object from the data as it knows what is the expected response object type.
3. Register the method for the request and response on lib-p2p host. e.g. (`/consensus/ping/request/1.0.0`, func OnPing)

For example, for the `Ping protocol` defined below, implement an `OnPingRequest` method to handle remote Pings and register it on `lib-p2p` host. Nodes can implement multiple versions of protocols and run them side-by-side as each implementation is versioned.

We need to sign the content of each message by its data generator node id to enable things like gossip where nodes forward messages they didn't create or signed to their peers. libp2p-secio only secures the comm channel between 2 nodes but we need to support gossip. e.g. nodes sending peers messages created by other nodes. There is  need for a `dispatcher` - responses contain request ids that allow impl handler to refer request data in the response. A factory method can assist in generating `MessageData`


We'll also need a method to return all protocols and versions supported by a node for negotiation purposes.

##### Signing requests and responses
1. Create the protobuf object with empty string for sig
2. Marshal to binary format to get bin data
3. Sign the bit data and add the sig to the sign field

##### Verifying signatures (request or response obj)
1. Copy sig and replace as empty string in sign field (sig)
2. Marshal object to bin format (data)
3. Verify bin data is signed with sig (data,sig)

#### handling incoming remote responses to locally sent messages (responses)

Callback is called by the p2p-lib host impls for the response protocol. The response includes the request id so impl can lookup the request data by that id. Impl may send a new request based on the remote response.


### Summary - protocol impl tasks
1. Create callback for handling remote requests. e.g. `onRemotePingRequest(data)` and register it for the method e.g. `pingservice/ping/req/1.0.0` (lib-p2p host streamHandler for a versioned protocol)

2. Create callback for handling remote responses. e.g. `pingservie/ping/resp/1.0.0` `onRemotePingResponse(data)`. Potentially, call a provided callback set for the request by its user.

3. Create stub for node app-logic to call a remote protocol method. Save request data and optional client callback by new message id.  e.g `Ping(peer, func callback)`. Message id generated for request data by factory method.

- Nodes register the protocols they support by registering a `multi-protocol` on the `lib-p2p` `host` and implementing protocol message handlers for processing RPC method calls.
- Nodes implement type to send messages using the protocol to one or more peers.

#### unruly p2p protocol over lib-p2p prototype:

Checkout /node/playground.go

----
