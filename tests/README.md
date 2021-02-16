#Automation Overview:

SM-automation is written in Python3.7 and uses pytest Python package for running tests on cloud machines that are managed by GKE (google kubernetes engine).
Each test folder holds a test file (test_testname.py) and a config yaml file that contains test configurations, k8s configurations and SM-client arguments.

Currently we use logs parsing in order to assert our tests but that will be changed to using the node's API.

####Packaging

Spacemesh code is being run on containers, miner container is defined in `DockerFile` file.
It includes a basic OS, environment variables, requirements (golang) and the compiled SM code.
When running a make command (locally or using the CI) another container is being created, this container is running the 
automation code and is defined under `DockerFileTests` file. It includes automation requirements (python, pytest, 
py packages, etc) and the automation code itself to be run on this pod.

###Kubernetes Files
   
K8s files are being used to deploy the networks' nodes (bootstrap node, client nodes, poet and curl).
Those files are located under `k8s` folder.
Important thing to know is that during the test before appliance these files are modified, it's being concatenated with
the k8s configuration in the test config file and changes that are made in the code itself.
You can see the changes being made in conftest.py under the functionality that deploys these files (setup bootstrap and
clients for example).

####SM-clients & Bootstrap
When deploying SM-client the bootstrap node will be deployed first, next all other clients (miners) will be deployed with the bootstrap as their `entry point` to the network.
Bootstrap node provides other nodes information about active nodes in the network.
After deployment, bootstrap node will function as a normal miner. 
After being deployed and part of the network, every client can perform as a bootstrap node for a new client that wants to join the network.
The number of replicas for each deployment (bootsrap or miners) can be found in the tests config file.

###Test Configurations

Each test has it's own configuration file.
This file is located at the same folder as the test and is named 'config.yaml' (except for late nodes test where it's
named `delayed_config.yaml`).
`config.yaml` consists of test configurations, k8s configurations and SM-client arguments which are being passed to the
node on activation.
The k8s configuration part of the config file will be concatenated to the k8s object files that are used by the test.

One important variable in the config files is the 'genesis_delta', this is the representation in seconds of the time
since deploying bootstrap node until the miners start mining, which means that this time should be sufficient for all
nodes to be ready.
If not ready by this time the test will fail.

####Running Automation Locally

Notice: Before running automation you need to have permissions to our GCP account management.
There is a document named 'instructions' in 1password with detailed instructions on how to setup all requirements for 
automation. 

There are two options for running automation locally:

* running pytest
python3 -m pytest -s -v path/to/your_test.py --tc-file=path/to/your_test_config.py --tc-format=yaml
this will run the local python automation code with the compiled miners 'develop' image.

* running make command
make dockertest-p2p-elk (for example)
when running a make command a new compiled image of the local branch will be created and run by the automation code.

####Logging
Logs can be viewed by accessing the local ELK cluster while the test is running.
Kibana's IP can be resolved by a kubectl command: `kubectl get services -n YOUE_NAMESPACE`
Logs will be dumped to a main ES server in two cases:
* the test run has failed
* "is_dump: True" was added to the test config file

In any other case the logs will be gone for good.

####Logical Test Objects Separation
When starting a new test a namespace will be created for that test and that test only.
All deployments, statefulset and other objects will be created under that namespace and that namepsace only.

####ELK Cluster
Automation ELK is built from Fluent-bit, Logstash, Kibana and Elasticsearch.
We use fluent-bit as a shipper for the logs from SM-client to Logstash and modify those logs there before moving them to ES.
After setting the ELK cluster we wait for it to be ready, this process might take a couple of minutes.

####Node pool
Node pools provide a physical test objects separation in oppose to namespacing.
When ELK is ready we add the node pool that will serve the Spacemesh pods, this node pool is necessary in order to get all of the resources needed (only for the SM-clients) in advance so our deployment will happen in a shorter and more predictable time.
The process of creating/deleting/altering a node pool is blocking since only one action can be made simultaneously, this means that if someone just started a test and wants to create a node pool he will have to wait for previous started action to end.
Node pool creation/deletion is polling every 10 seconds to see if it can start, in case it can't (for the reason mentioned above) it creates a single `Error` message for the first time it tries, this message can be ignored for it will start once GCP is ready to take another action.

####Communicating With Miners
In order to communicate with SM-clients we use a CURL pod.
Actions like sending a transaction or requesting a node balance/nonce etc, will be performed by sending an http request to one or many of the SM-clients.
Another CURL use in automation is to start the poet using its API what makes it necessary for each test that use a POET service.
