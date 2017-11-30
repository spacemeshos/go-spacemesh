## Unruly p2p Node Design

### `Node`
An Internet-connected computer running the `Unruly Node` software. Each node is identified by a public `Node Id`. The `Node Id` is derived from or be the public key in a key-pair generated on node first run and persisted in node's settings file.

- Node pub and priv keys contains a 256 bits `Secp256k1` `key data` and the `key type` (`Secp256k1`).
- A key `binary representation` is protobufs formatted bytes of the key data and type.
- A keys `string representation` is a `base58` encoding of the key `binary representation`.

- `Node id` uniquely identifies a p2p node. It is derived from the node's `public key`
- `Node id` include a `sha256` of the node's `public key` (256 bits long) and a hash type (sha256).
- `Node id` binary representation is a multi-hash binary encoding of the id data and type
- `Node id` string is a `base56` encoding of the node id binary representation

Crypto question: Unless we decide not to rely on `FIPS 180-4` (like Eth with their non-standard sha-256-like `Keccak256` due to NSA backdoor concerns) we plan to use `Go/Crypto` lib for crypto and `Go/crypto/sha256` for sha-256 implementations.

#### Some examples
- Public key binary rep total bytes: 37 (32 for a Secp256k1 key data + 5 for meta-data)
- Public key (base58): `GZsJqUUbMHHEY8zMnJDkbuitZf3nvW91otcKvrA5jVoa31XcAH` - 50 bytes
- Node id binary rep total bytes: 34 (32 for a sha256 hash + 2 for type)
- Node id (base58): `QmTraazvQHPy7X7nkLrJjPZ6ubwa3r7RfpvsMpHCTjvdyt` - 46 bytes


### `Peer`
A peer to a node, is a node which is connected to it over the Internet. Nodes may send `p2p messages` directly to their peers. Note that the nodes may not be connected directly using an underlying connection as we are working on an `overlay network` situation. So each `p2p message` is coming from a peer but may be authored by another node on the network. Our p2p code supports full authentication of the message's sender and the message's author.
For authenticating senders, we rely on `lib-p2p` transport authentication. For author authentication we realy on our own framed p2p protocol.

### `Accounts`

#### Users Accounts
A `user account` belongs to a real user who has access to the account's private key and passphrase. It is created via the `node api` using `ECC`. e.g. User generates a `secp256k1` key pair and the account identity is based on the public key.

Maintains the user's `Unrulies` balance.

Maintains the user's state for `Smart Contract Templates`

`Identity': established based on the account's public key. (In Ethereum an account id is the last 20 bytes of the public key). Unruly identity is a base58 string of the account public key.

Account Id is a `sha256` hash of his `secp256k1` public key.

Account Id string representation is a `base58` encoding of the account id.

Nonce - incremented when a user's transaction is added to a block. Used to avoid adding more than one copy of user's transaction in a block.

Each account has a `key store` file which contains the account id, private key and salt. The private key is encrypted using the user's passphrase in store.

A node has access to key store files (one per account) (at well-known dir) so it can accept passphrase as method param to unlock any such account for a runtime session.

Node may be command-line started with a list of account ids to unlock and to set the `coinbase account` (used for mining awards). the `coinbase account` is a default account for usage in a session. It must have a key store file.

Node API methods will have versions that accept account signature on input data - this allows node to execute a method on behalf of an account without access to the account private key. e.g. send a transaction to the network on behalf of the user. (This is how Infura works together with MetaMask and other eth wallets).

#### Smart Contract Accounts
In addition to user account data, a smart contract account includes:
- deployed smart contract code
- deployed version #
- Current smart contract state
- Deployer user account id
- Same pub/private key and id setup as user account

#### Smart Contract Templates
A smart contract template is a deployed smart contract code without singleton state. The state of template instances are managed under user accounts. In other words, the template provides the deployed code and the state differs per user. This is useful for scenarios where the same smart contract is used by millions of users. Using template, 1 million copies of the code don't need to be deployed on the blockchain - only 1. The state is maintained for each smart code user.

#### Security Considirations

##### Account Key File (stored at rest)
- User account private key should be stored encrypted with the user's passphrase on disk and salted. This provides some security against attackers which gain access to the key store file. The passphrase should only be stored in the user's password wallet (ideally both on his PC and at least 1 mobile device). Users are encouraged to backup their account's key store files. Each file has data about 1 account. File name should be `{user base58 account id}.json`

##### Key file data format

For sake of saving development time we are considering to reuse most of eth key store file format and private key encryption using scrypt, a password and a salt.

```json
{"accountPublicKey":"base58-encoded-account-pub-key(Scep256k1 256 bits)",
  "accountId":"account-id--is-a-base-58-encoded-sha256-of-public-key-raw-binary-format",
  "version":1,
  "crypto":{
    "cipher":"aes-128-ctr",
    "ciphertext":"0f6d343b2a34fe571639235fc16250823c6fe3bc30525d98c41dfdf21a97aedb",
    "cipherparams":{
      "iv":"cabce7fb34e4881870a2419b93f6c796"
    },
    "kdf":"scrypt",
    "kdfparams":{
      "dklen":32,
      "n":262144,
      "p":1,
      "r":8,
      "salt":"1af9c4a44cf45fe6fb03dcc126fa56cb0f9e81463683dd6493fb4dc76edddd51"
    },
    "mac":"5cf4012fffd1fbe41b122386122350c3825a709619224961a16e908c2a366aa6"
  }
}
```

Refs
https://github.com/ethereum/go-ethereum/wiki/Passphrase-protected-key-store-spec
https://github.com/Gustav-Simonsson/go-ethereum/blob/improve_key_store_crypto/crypto/key_store_passphrase.go#L29

- We need to do more research to figure out if we really need to use `scrypt` here or we can just make the cipher private key a sha256(salt||user-password).

- Technically, both account id and public key can be derived from the private key but it is useful to store them in the key store file so users can see to which accounts they have private keys for by looking at the content of the file. The file name will also be `{account-id-str}.json` to make it easy to backup keys for each account

- Users must maintain both their passphrase as well as a backup of the key file. Without both they lose access to their account. They are encouraged to backup both to a secure password vault on at least 2 devices.

##### Account passphrase
User provided. Not persisted in the key store file. Users are recommend to generate these using a strong random password generator such as 1-password. We'd like to avoid having users deal with a 12-phrase nemonic and a passphrase. The node software should also support strong random password generation option to minimize the number of weak user-generated passwords (a common thing).

- To access an account private key, user must provide: (passphrase, salt, kdfparams params, cypher init vector). Passphrase is something user has to remember / know, the other params are something user has to have - a keystore file.

##### Unlocking an account
- Enable node to have access to the account's private key (and sign messages / transactions with it) while it is running. Needed for validation as well.
- Alternative is to provide passphrase as param to submit a tx or to submit a signed transaction via the api.

-----

### p2p Networking requirements

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

