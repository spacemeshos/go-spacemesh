
# ################ MESH ################


def parse_layer_hash_from_layer_query_res(layer_query):
    # {'layer': ['hash': '[\d\w=]+']}
    return layer_query['layer'][0]['hash']


def parse_layer_per_epoch_msg(msg):
    # {'numlayers': {'value': '\d+'}}
    return msg['numlayers']['value']


def parse_layer_number(msg):
    # {'layernum': {'number': '\d+'}}
    return int(msg['layernum']['number'])


def parse_current_epoch(msg):
    # {'epochnum': {'value': '\d+'}}
    if 'value' not in msg['epochnum']:
        # epoch 0 returns - msg = {'epochnum': {}}
        return 0
    return msg['epochnum']['value']


# ################ SMESHER ################


def parse_smesher_id(msg):
    # {'account_id': {'address': 'WMW/28wYNZYfJtd9ZCHdiBALGFB6wiDuyhnKsDA4YHs='}}
    return msg['account_id']['address']

