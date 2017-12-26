## SpaceMesh data folders structure

- `spacemesh-data`/   
    - `nondes/`  
       - `[node-Id1]`
	        - id.json   
	        - logs/
	        - ...
        - `[node-Id2]`
	        - id.json
	        - logs/
	        - ...
	    - ....
    - accounts/
        - `[user1-id-base58].json`
	    - // key files go here and shared between all nodes
	- logs/
	    - Any non-node specific log files go here


- `spacemesh-data` is the master directory were all data is persisted. 
- It has a default location per OS and can be configured in the config file.
- At the top level, it contains a accounts/ dir which includes all accounts data files.
- A dir is created for each node instance and is named by its id.
- All node persistent data goes in the node's dir
- All ids are base58 encoded strings of binary node and account ids (public keys)
- We do not use ids which are hashes of public keys. Spacemesh ids are public keys.



