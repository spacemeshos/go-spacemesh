# this will be the script to run from gcp

# if __name__ == "__main__":
#     # execute only if run as a script
#     gen = TxGenerator()
#     data = gen.generate("0000000000000000000000000000000000001111", 12345, 56789, 24680, 86420)
#     # data = gen.generate("0000000000000000000000000000000000002222", 0, 123, 321, 100)
#     # x = (str(list(data)))
#     # print('{"tx":'+ x + '}')
#
#     expected = "00000000000030390000000000000000000000000000000000001111000000000000ddd500000000000060680000000000" \
#                "01519417a80a21b815334b3e9afd1bde2b78ab1e3b17932babd2dab33890c2dbf731f87252c68f3490cce3ee69fd97d450" \
#                "d97d7fcf739b05104b63ddafa1c94dae0d0f"
#     assert (binascii.hexlify(data)).decode('utf-8') == str(expected)
