def str2bool(v):
    if isinstance(v, bool):
        return v
    if v.lower() in ('yes', 'true', 't', 'y', '1'):
        return True
    elif v.lower() in ('no', 'false', 'f', 'n', '0'):
        return False
    else:
        raise ValueError('Boolean value expected.')


def validate_params_in_dict(_dict, params_list):
    if not isinstance(_dict, dict):
        raise ValueError(f"input is not type dict but of type: {type(_dict)}")
    for param in params_list:
        if param not in _dict:
            msg = f"missing {param} argument"
            raise ValueError(msg)
