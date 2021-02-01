#Automation overview:

SM-automation is written in Python3.7 and uses pytest Python package for running tests on cloud machines that are managed by GKE (google kubernetes engine).\
Each test folder holds a test file (test_testname.py) and a config yaml file that contains test configurations, k8s configurations and SM-client arguments.

Currently we use logs parsing in order to assert our tests but that will be changed to using the node's API.

####Logical Test Objects Separation
When starting a new test a namespace will be created for that test and that test only.\
All deployments, statefulset and other objects will be created under that namespace and that namepsace only.

####ELK Cluster
Automation ELK is built from Fluent-bit, Logstash, Kibana and Elasticsearch.\
We use fluent-bit as a shipper for the logs from SM-client to Logstash and modify those logs there before moving them to ES.\
After setting the ELK cluster we wait for it to be ready, this process might take a couple of minutes.

####Node pool
Node pools provide a physical test objects separation in oppose to namespacing.\
When ELK is ready we add the node pool that will serve the Spacemesh pods, this node pool is necessary in order to get all of the resources needed (only for the SM-clients) in advance so our deployment will happen in a shorter and more predictable time.\
The process of creating\deleting\altering a node pool is blocking since only one action can be made simultaneously, this means that if someone just started a test and wants to create a node pool he will have to wait for previous started action to end.\
Node pool creation\deletion is polling every 10 seconds to see if it can start, in case it can't (for the reason mentioned above) it creates a single `Error` message for the first time it tries, this message can be ignored for it will start once GCP is ready to take another action.

####Curl
In order to communicate with SM-clients we use a CURL pod.\
Actions like sending a transaction or requesting a node balance\nonce etc, will be performed by sending an http request to one or many of the SM-clients.
Another CURL use in automation is to start the poet using its API what makes it necessary for each test that use a POET service.

####SM-clients
When deploying SM-client the bootstrap node will be deployed first, next all other clients (miners) will be deployed with the bootstrap as their `entry point` to the network.\
Bootstrap node provides other nodes information about active nodes in the network and the IP of the POET service.\
After deployment, bootstrap node will function as a normal miner, same, every client can perform as a bootstrap node for a new client.\
The number of replicas for each deployment (bootsrap or miners) can be found in the tests config file.

####Logging
Logs can be viewed by accessing the local ELK cluster while the test is running.\
Kibana's IP can be resolved by a kubectl command: `kubectl get services -n YOUE_NAMESPACE`
Logs will be dumped to a main ES server in two cases:
* the test run has failed
* "is_dump: True" was added to the test config file

In any other case the logs will be gone for good.
