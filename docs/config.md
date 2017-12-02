## Unruly Config
- We are using [toml files](https://github.com/toml-lang/toml), [here](https://github.com/urfave/cli#values-from-alternate-input-sources-yaml-toml-and-others) and [here](https://github.com/pelletier/go-toml) for config file syntax.
- Use of a config file is optional.
- Node uses default config values, unless provided with a .toml config file via command line arg or has custom command line config values.
- We might provide an example toml config file.
- App shell supports flags and commands. Flags override default config values. Commands execute any code module that might modify config values.
- Params are non-configurable hard-coded consts. Ideally all params should become configurable configs.
- Commands are specified as app args
- It is important to separate modiles flags, params and commands
- Each major new module should implement its own flags, params, and cli commands - see node for a ref impl.

