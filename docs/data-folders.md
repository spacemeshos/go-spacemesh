## Unruly data folders structure

- unruly-data/     
    - `[nodeId1]`
	    - data/
	        - logs/
    - `[nodeId2]`
	    - data/
	        - logs/
    - keys/
        - `[user1-id-base58].josn`
	    - // key files go here and shared between all nodes
	- logs/
	    - Any non-node specific log files go here


- unruly-data is the master folder were all data is persisted. It has a default location per OS and can be configured in the config file.
At the top level, it contains a key/ folder which includes all keystore files.
A folder is created for each node instance and is named by its id.

- All ids are base58 encoded strings of id binary data



