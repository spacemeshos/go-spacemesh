import pprint
import random

from tests.ed25519.eddsa import genkeypair
import tests.tx_generator.config as conf


class Accountant:
    ACCOUNT = {"priv": "", "balance": 0, "nonce": 0, "recv": [], "send": []}
    RECV = {"from_acc": "", "amount": 0, "gasprice": 0}
    SEND = {"to": "", "amount": 0, "gasprice": 0}

    def __init__(self, accounts=None, tx_cost=conf.tx_cost):
        self.accounts = accounts if accounts else {}
        self.tx_cost = tx_cost

    def calc_balance(self, acc_pub, init_amount=0, debug=False):
        """
        calculate the account's balance

        :param acc_pub: string, account's public key
        :param init_amount: int, initial amount
        :param debug: bool, print balance or not

        :return: int, the balance after sending and receiving txs
        """

        balance = sum([int(tx["amount"]) for tx in self.accounts[acc_pub]["recv"]]) - \
                  sum(int(tx["amount"]) + (int(tx["gasprice"]) * self.tx_cost) for tx in self.accounts[acc_pub]["send"])

        if debug:
            print(f"balance calculated for {acc_pub}:\n{balance}\n"
                  f"everything:\n{pprint.pformat(self.accounts[acc_pub])}")

        self.accounts[acc_pub]["balance"] = balance + init_amount
        return balance + init_amount

    def new_account(self):
        """
        create a new account and adds it to the accounts data structure

        :return: string, public key of the new account
        """

        priv, pub = genkeypair()
        str_pub = bytes.hex(pub)
        self.accounts[str_pub] = self.set_account(bytes.hex(priv), 0, 0, [], [])
        return str_pub

    def random_account(self):
        pk = random.choice(list(self.accounts.keys()))
        return pk

    # ============= properties =============

    def set_acc_send(self, acc, send):
        self.accounts[acc]["send"].append(send)
        init_amount = conf.init_amount if acc == conf.acc_pub else 0
        self.calc_balance(acc, init_amount)

    def set_acc_recv(self, acc, recv):
        self.accounts[acc]["recv"].append(recv)
        init_amount = conf.init_amount if acc == conf.acc_pub else 0
        self.calc_balance(acc, init_amount)

    def get_acc_priv(self, acc):
        return self.accounts[acc]["priv"]

    def get_acc(self, acc_pub):
        return self.accounts[acc_pub]

    def get_nonce(self, acc_pub):
        return self.accounts[acc_pub]["nonce"]

    def set_nonce(self, acc_pub, jump=1):
        self.accounts[acc_pub]["nonce"] += jump
        return self.accounts[acc_pub]["nonce"]

    def get_balance(self, acc_pub):
        return self.accounts[acc_pub]["balance"]

    # =============== utils ================

    @staticmethod
    def new_untracked_account():

        """
        create a new account without adding it to an accounts data structure

        :return: string, string: public and private key of the new account
        """

        priv, pub = genkeypair()
        str_pub = bytes.hex(pub)
        str_priv = bytes.hex(priv)
        print(f"generated new untracked account:\npub: {str_pub}\npriv: {str_priv}")
        return str_pub, str_priv

    @staticmethod
    def set_tap_acc():
        return dict(Accountant.ACCOUNT, balance=conf.init_amount, priv=conf.acc_priv, recv=[], send=[])

    @staticmethod
    def set_account(priv, balance=0, nonce=0, recv=None, send=None):
        receive = [] if not recv else recv
        send_lst = [] if not send else send
        return dict(Accountant.ACCOUNT, balance=balance, priv=priv, nonce=nonce, recv=receive, send=send_lst)

    @staticmethod
    def set_recv(_from, amount, gasprice):
        return dict(Accountant.RECV, from_acc=_from, amount=amount, gasprice=gasprice)

    @staticmethod
    def set_send(to, amount, gasprice):
        return dict(Accountant.SEND, to=to, amount=amount, gasprice=gasprice)
