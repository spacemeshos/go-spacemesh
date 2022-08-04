package config

type BlockCertificateConfig struct {
    CommitteeSize  int // from hare config
    MaxAdversaries int // from hare config
    Hdist          int // from tortoise config
}
