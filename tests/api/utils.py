import base64


def base64_to_hex(b64):
    return base64.b64decode(b64).hex()


def hex_to_base64(hexa):
    return base64.b64encode(bytes.fromhex(hexa)).decode()