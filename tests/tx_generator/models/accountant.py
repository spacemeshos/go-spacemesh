import random

from tests.ed25519.eddsa import genkeypair
import tests.tx_generator.config as conf


class Accountant:
    """
    This object follows and records the current state since it has been created,
    all new accounts and their info including nonce, balance etc.

    """

    ACCOUNT = {"priv": "", "balance": 0, "nonce": 0, "recv": [], "send": []}
    RECV = {"from_acc": "", "amount": 0, "gasprice": 0}
    SEND = {"to": "", "amount": 0, "gasprice": 0}

    def __init__(self, accounts=None, tx_cost=conf.tx_cost, tap_init_amount=conf.init_amount):
        self.accounts = accounts if accounts else {}
        self.tap_init_amount = tap_init_amount
        self.tx_cost = tx_cost

    def calc_balance(self, acc_pub, init_amount=0):
        """
        calculate the account's balance

        :param acc_pub: string, account's public key
        :param init_amount: int, initial amount

        :return: int, the balance after sending and receiving txs
        """

        balance = sum([int(tx["amount"]) for tx in self.accounts[acc_pub]["recv"]]) - \
                  sum(int(tx["amount"]) + (int(tx["gasprice"]) * self.tx_cost) for tx in self.accounts[acc_pub]["send"])

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

    def set_accountant_from_queue(self, queue):
        queue.put("DONE")
        while True:
            account_details = queue.get()
            if account_details == "DONE":
                break

            lst_key = account_details[0]
            acc_pub = account_details[1]
            entry = account_details[2]

            if lst_key == "send":
                self.set_sending_acc_after_tx(acc_pub, entry)
            elif lst_key == "recv":
                self.set_receiving_acc_after_tx(acc_pub, entry)
            else:
                raise ValueError(f"no such list \'{lst_key}\' in Account")

    def random_account(self):
        pk = random.choice(list(self.accounts.keys()))
        return pk

    # ============= properties =============

    def set_sending_acc_after_tx(self, frm, send_entry):
        self.set_nonce(frm)
        self.append_to_acc_send(frm, send_entry)

    def set_receiving_acc_after_tx(self, to, recv_entry):
        self.append_to_acc_recv(to, recv_entry)

    def append_to_acc_send(self, acc, send):
        self.accounts[acc]["send"].append(send)
        init_amount = self.tap_init_amount if acc == conf.acc_pub else 0
        self.calc_balance(acc, init_amount)

    def append_to_acc_recv(self, acc, recv):
        self.accounts[acc]["recv"].append(recv)
        init_amount = self.tap_init_amount if acc == conf.acc_pub else 0
        self.calc_balance(acc, init_amount)

    def get_acc_send_lst(self, acc_pub):
        return self.accounts[acc_pub]["send"]

    def get_acc_recv_lst(self, acc_pub):
        return self.accounts[acc_pub]["recv"]

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
    def set_tap_acc(balance=conf.init_amount, nonce=0):
        return dict(Accountant.ACCOUNT, balance=balance, priv=conf.acc_priv, nonce=nonce, recv=[], send=[])

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
