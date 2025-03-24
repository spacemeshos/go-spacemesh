# Network decentralization improvements

## Description

Incentivize re-initialization by giving identities that have been created after a certain epoch extra weight in their ATXs.

## Acceptance criteria

- Add a new config parameter `BonusWeightEpoch` that defines the epoch at which new identities start to receive extra
  weight
  - to be eligible for the extra weight an identity must use a commitment ATX that isn't older than
    `BonusWeightEpoch-2`, any ATX published in or after that epoch and is used as commitment makes an identity eligible
    for the extra weight
  - **TODO:** specify which epoch will be the first giving extra weight on mainnet
- Weight calculation is updated to use the current formula for all identities with a commitment ATX that is older than
  `BonusWeightEpoch-2` or the golden ATX
  - the new formula is used for all new identities **TODO:** specify new formula

## Implementation hints

- For `fastnet` use epoch 3 as the `BonusWeightEpoch` so that all identities created after genesis receive the bonus
  weight
- For existing testnets the `BonusWeightEpoch` should be set to an epoch in the near future, but not hard coded into
  `testnet` preset.
