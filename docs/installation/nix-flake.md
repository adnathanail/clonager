# Nix flake

The flake's package installs clonager as both `clonager` and `cg`, with bash,
fish and zsh completions for each. Its Home Manager module
(`homeModules.default`) installs the package and the config; see the
[README](../../README.md#with-nix) for an example. Its options, under
`programs.clonager`:

| Option | |
|---|---|
| `enable` | install clonager |
| `package` | the clonager package to install (default this flake's) |
| `configFile` | the config, installed as `~/.config/clonager/config.yaml`. A path (`./clonager.yaml`) is copied into the Nix store; a string (`"/run/agenix/clonager-config"`) is linked to where it is |
| `configSource` | where `clonager config` commands read and write the config: an absolute path to it in your checkout (a string, not a path, so it isn't copied into the store), or `decrypt`/`encrypt` commands (see [below](#keeping-the-config-encrypted)) |
| `discoverPaths` | where `clonager config discover` looks when given no dirs |
| `openIn` | what cmd-clicking a repo's name opens it in: `vscode`, `cursor`, `zed`, `files` (the file manager, the default) or `none` |
| `alwaysOffline` | make `clonager` skip checking GitHub, as with `--offline` (default `false`) |

## Applying config changes

The installed config is a read-only copy, so it only changes when you rebuild.
With `configSource` set, `clonager config discover` adds new repos to that
file instead, so they show up as a diff in your config repo; rebuild to apply them.
Until you do, `clonager` reminds you there are changes not applied yet.
Without `configSource`, the `config` commands refuse to touch a read-only
config.

If you already have a `~/.config/clonager/config.yaml`, move it into your config
repo (as `clonager.yaml` in the [README example](../../README.md#with-nix)) before rebuilding, or Home Manager will refuse
to replace it.

## Keeping the config encrypted

The config lists every repo's URL, which you may not want in a public config
repo. To keep it encrypted (e.g. with [agenix](https://github.com/ryantm/agenix)),
give `configSource` commands instead of a path, and point `configFile` at the
decrypted copy, as a string so it's linked to rather than copied into the
store:

```nix
programs.clonager = {
  enable = true;
  configFile = "/run/agenix/clonager-config";  # decrypted at activation
  configSource = {
    # Prints the config.
    decrypt = "cd ~/.config/nix-darwin/secrets && agenix -d clonager.age -i ~/.config/age/keys.txt";
    # Reads the new config on stdin, and stores it.
    encrypt = "cd ~/.config/nix-darwin/secrets && agenix -e clonager.age -i ~/.config/age/keys.txt";
  };
};
```

Commands that change the config (`clonager config ...`) then decrypt it, make their
changes in memory, and encrypt it again, so the plaintext is never written to
disk. `encrypt` only runs if something changed, since re-encrypting would
change the encrypted file even when its contents are the same. Neither command
may prompt; `clonager` runs `decrypt` too, to tell you about changes not
applied yet.
