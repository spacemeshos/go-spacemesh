## Unruly Node Config
- We are using [toml files](https://github.com/toml-lang/toml) for config file syntax.
- Use of a config file is optional. 
- Node uses default config values, unless provided with a .toml config file via command line arg or has custom command line config values.
- We might provide an example toml config file.
- App shell supports flags and commands. Flags override default config values. Commands execute any code module that might modify config values.
