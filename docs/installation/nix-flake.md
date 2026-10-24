## Nix flake

The installed config is a read-only copy, so it only changes when you rebuild.
With `configSource` set, `clonager config discover` adds new repos to that
file instead, so they show up as a diff in your config repo; rebuild to apply them.
Until you do, `clonager` reminds you there are changes not applied yet.
Without `configSource`, the `config` commands refuse to touch a read-only
config.

If you already have a `~/.config/clonager/config.yaml`, move it into your config
repo (as `clonager.yaml` above) before rebuilding, or Home Manager will refuse
to replace it.

### Keeping the config encrypted

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