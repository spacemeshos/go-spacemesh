import binascii
import pprint
import random
import time
import xdrlib

from tests.ed25519.eddsa import signature2, genkeypair
import binascii
import pprint
import random
import time
import xdrlib

from tests.ed25519.eddsa import signature2, genkeypair

ADDRESS_SIZE = 20
SIGNATURE_SIZE = 64


class TxGenerator:
    def __init__(self, pub="7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c",
                 pri="81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce5"):
        self.publicK = bytes.fromhex(pub)
        self.privateK = bytes.fromhex(pri)

    def generate(self, dst, nonce, gasLimit, fee, amount):
        p = xdrlib.Packer()
        p.pack_hyper(nonce)
        dstBytes = bytes.fromhex(dst)
        addr = dstBytes[len(dstBytes) - 20:]
        p.pack_fstring(ADDRESS_SIZE, addr)
        p.pack_hyper(gasLimit)
        p.pack_hyper(fee)
        p.pack_hyper(amount)

        data = p.get_buffer()
        sign = signature2(data, self.privateK)
        p.pack_fstring(SIGNATURE_SIZE, sign)

        return p.get_buffer()


if __name__ == "__main__":
    # execute only if run as a script
    gen = TxGenerator()
    data = gen.generate("0000000000000000000000000000000000001111", 12345, 56789, 24680, 86420)
    # data = gen.generate("0000000000000000000000000000000000002222", 0, 123, 321, 100)
    # x = (str(list(data)))
    # print('{"tx":'+ x + '}')

    expected = "00000000000030390000000000000000000000000000000000001111000000000000ddd50000000000006068000000000001519417a80a21b815334b3e9afd1bde2b78ab1e3b17932babd2dab33890c2dbf731f87252c68f3490cce3ee69fd97d450d97d7fcf739b05104b63ddafa1c94dae0d0f"
    assert (binascii.hexlify(data)).decode('utf-8') == str(expected)


class WalletAPI:
    DEBUG = False

    def __init__(self, testconfig, api_call):
        self.testconfig = testconfig
        self.api_call = api_call

    nonceapi = 'v1/nonce'
    submitapi = 'v1/submittransaction'
    balanceapi = 'v1/balance'

    def SubmitTx(self, to, src, gaslimit, gasprice, amount, txgen):
        pod_ip, pod_name = self.random_node()
        txBytes = txgen.generate(to, src, gaslimit, gasprice, amount)
        print("submit transaction from {3} to {0} of {1} with gasprice {2}".format(to, amount, gasprice, src))
        data = '{"tx":' + str(list(txBytes)) + '}'
        out = self.api_call(pod_ip, data, self.submitapi, self.testconfig['namespace'])
        if self.DEBUG:
            print(data)
            print(out)
        return "{'value': 'ok'}" in out

    def CheckNonce(self, acc):
        # check nonce
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + acc + '"}'
        print("checking {0} nonce".format(acc))
        return self.api_call(pod_ip, data, self.nonceapi, self.testconfig['namespace'])

    def GetBalance(self, acc):
        # check balance
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + str(acc) + '"}'
        if self.DEBUG:
            print(data)
        out = self.api_call(pod_ip, data, self.balanceapi, self.testconfig['namespace'])
        return out

    def random_node(self, setup_network):
        # rnd = random.randint(0, len(setup_network.clients.pods)-1)
        return setup_network.clients.pods[0]['pod_ip'], setup_network.clients.pods[0]['name']


#layers_per_ep = testconfig['client']['args']['layers-per-epoch']
#layers_dur = testconfig['client']['args']['layer-duration-sec']


def test_transactions(walletApi, layers_per_epoch, layers_duration):
    DEBUG = False

    tap_init_amount = 10000
    tap = "7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

    tx_cost = 3  # .Mul(trans.GasPrice, tp.gasCost.BasicTxCost)

    accounts = {
        tap: {"priv": "81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce5", "nonce": 0, "send": [],
              "recv": []}}

    def expected_balance(acc):
        balance = sum([int(tx["amount"]) for tx in accounts[acc]["recv"]]) - sum(
            int(int(tx["amount"]) + (int(tx["gasprice"]) * tx_cost)) for tx in accounts[acc]["send"])
        if DEBUG:
            print("balance calculated for {0}, {1}, everything: {2}", acc, balance, pprint.pformat(accounts[acc]))
        return balance

    def random_account():
        pk = random.choice(list(accounts.keys()))
        return pk

    def new_account():
        priv, pub = genkeypair()
        strpub = bytes.hex(pub)
        accounts[strpub] = {"priv": bytes.hex(priv), "nonce": 0, "recv": [], "send": []}
        return strpub

    def transfer(frm, to, amount=None, gasprice=None, gaslimit=None):
        txGen = TxGenerator(pub=frm, pri=accounts[frm]['priv'])
        if amount is None:
            amount = random.randint(1, expected_balance(frm) - (1 * tx_cost))
        if gasprice is None:
            gasprice = 1
        if gaslimit is None:
            gaslimit = gasprice + 1
        success = walletApi.SubmitTx(to, accounts[frm]['nonce'], gaslimit, gasprice, amount, txGen)
        accounts[frm]['nonce'] += 1
        if success:
            accounts[to]["recv"].append({"from": bytes.hex(txGen.publicK), "amount": amount, "gasprice": gasprice})
            accounts[frm]["send"].append({"to": to, "amount": amount, "gasprice": gasprice})
            return True
        return False

    def test_account(acc, init_amount=0):
        out = walletApi.CheckNonce(acc)
        if DEBUG:
            print(out)
        if str(accounts[acc]['nonce']) in out:

            if DEBUG:
                print(out)
            balance = init_amount
            balance = balance + expected_balance(acc)
            print("expecting balance: {0}".format(balance))
            if "{'value': '" + str(balance) + "'}" in out:
                print("{0}, balance ok ({1})".format(str(acc), out))
                return True
            return False
        return False

    # send tx to client via rpc
    TEST_TXS = 10
    for i in range(TEST_TXS):
        balance = tap_init_amount + expected_balance(tap)
        if balance < 10:  # Stop sending if the tap is out of money
            break
        amount = random.randint(1, int(balance / 2))
        strpub = new_account()
        print("TAP NONCE {0}".format(accounts[tap]['nonce']))
        assert transfer(tap, strpub, amount=amount), "Transfer from tap failed"
        print("TAP NONCE {0}".format(accounts[tap]['nonce']))

    ready = 0
    for x in range(int(layers_per_epoch) * 2):  # wait for two epochs (genesis)
        ready = 0
        print("...")
        time.sleep(float(layers_duration))
        print("checking tap nonce")
        if test_account(tap, tap_init_amount):
            print("nonce ok")
            for pk in accounts:
                if pk == tap:
                    continue
                print("checking account")
                print(pk)
                assert test_account(pk), "account {0} didn't have the expected nonce and balance".format(pk)
                ready += 1
            break
    assert ready == len(accounts) - 1, "Not all accounts received sent txs"  # one for 0 counting and one for tap.

    def is_there_a_valid_acc(min_balance, excpect=[]):
        for acc in accounts:
            if expected_balance(acc) - 1 * tx_cost > min_balance and acc not in excpect:
                return True
        return False

    ## IF LOGEVITY THE CODE BELOW SHOULD RUN FOREVER

    TEST_TXS2 = 10
    newaccounts = []
    for i in range(TEST_TXS2):
        if not is_there_a_valid_acc(100, newaccounts):
            break

        if i % 2 == 0:
            # create new acc
            acc = random_account()
            while acc in newaccounts or expected_balance(acc) < 100:
                acc = random_account()

            pub = new_account()
            newaccounts.append(pub)
            assert transfer(acc, pub), "Transfer from {0} to {1} (new account) failed".format(acc, pub)
        else:
            accfrom = random_account()
            accto = random_account()
            while accfrom == accto or accfrom in newaccounts or expected_balance(accfrom) < 100:
                accfrom = random_account()
                accto = random_account()
            assert transfer(accfrom, accto), "Transfer from {0} to {1} failed".format(accfrom, accto)

    ready = 0

    for x in range(int(layers_per_epoch) * 3):
        time.sleep(float(layers_duration))
        print("...")
        ready = 0
        for pk in accounts:
            if test_account(pk, init_amount=tap_init_amount if pk is tap else 0):
                ready += 1

        if ready == len(accounts):
            break

    assert ready == len(accounts), "Not all accounts got the sent txs got: {0}, want: {1}".format(ready,
                                                                                                  len(accounts) - 1)
