# Termux setup and phone operations

This guide describes optional hosting on a compatible Android device with ARM64
Termux. It covers ADB and SSH access, provider artifacts, startup, dashboard login
and recovery. For a new account, start with the Compose configuration in the
[README](../README.md). Preserve identity, credentials and database roles when
transferring an existing account.

## Android and Termux prerequisites

1.
Install Termux and Termux:Boot from the same source, such as F-Droid.
Their
   signatures must match.
Open both apps once after installation.
2.
Set battery use to unrestricted for both apps and exclude them from sleeping
   apps.
Enable Developer options and USB debugging; authorize the computer's RSA
   prompt with the phone unlocked.
3.
In Termux, acquire the wake lock before starting background services:

```sh
termux-wake-lock
pkg update
pkg upgrade
pkg install python openssh proot-distro runit cloudflared postgresql git
whoami
passwd
sshd
```

Record the `whoami` result, such as `u0_aNNN`.
OpenSSH normally listens on port
8022.
The native PostgreSQL package supplies client tools; the database service can
use the separate matching Debian runtime described below.
`passwd` establishes a bootstrap
SSH password; use a dedicated SSH key for regular access.

## Log into Termux through ADB and SSH

Install [Android platform tools](https://developer.android.com/tools/releases/platform-tools)
on the computer.
With one authorized phone connected over USB:

```sh
adb devices -l
adb forward tcp:18022 tcp:8022
```

`adb devices` must show `device`, rather than `unauthorized` or `offline`.
Unlock
and accept the RSA prompt if necessary.
With multiple devices, add `-s SERIAL`
right after `adb` to select this phone explicitly.

**`adb shell` is Android's shell user, not the Termux app user.** It can inspect
Android settings but does not give access to Termux's private home or Debian
containers.
Use the forwarded SSH port to log into Termux:

```sh
phone_user=u0_aNNN # Replace with the actual Termux whoami result.
ssh -p 18022 "$phone_user"@127.0.0.1
```

On a new computer, create a dedicated key only if it does not already exist:

```sh
ssh-keygen -t ed25519 -f ~/.ssh/mcp_termux_ed25519 -C bifrost-phone
```

Append its public key without replacing existing authorized keys.
This first SSH
connection uses the bootstrap password entered in Termux:

```sh
ssh -p 18022 "$phone_user"@127.0.0.1 'umask 077
mkdir -p ~/.ssh
cat >> ~/.ssh/authorized_keys
chmod 700 ~/.ssh
chmod 600 ~/.ssh/authorized_keys' < ~/.ssh/mcp_termux_ed25519.pub

ssh -i ~/.ssh/mcp_termux_ed25519 -p 18022 "$phone_user"@127.0.0.1
```

Verify the host-key fingerprint before trusting the first connection.
In Termux,
its public key can be inspected with:

```sh
ssh-keygen -lf "$PREFIX/etc/ssh/ssh_host_ed25519_key.pub"
```

The existing phone also has the `termux` SSH alias through its Cloudflare tunnel:

```sh
ssh -i ~/.ssh/mcp_termux_ed25519 termux
```

For wireless debugging on Android 11+, connect the phone and computer to the same
network.
In Developer options, open **Wireless debugging → Pair device with
pairing code**, then use the displayed pairing address and code:

```sh
adb pair PHONE_IP:PAIRING_PORT
adb connect PHONE_IP:DEBUGGING_PORT
adb devices -l
adb forward tcp:18022 tcp:8022
```

Pairing and debugging ports differ.
The debugging port can change after reconnect
or reboot.
Recreate port forwards after the device reconnects; then use the same
SSH commands.
See the [ADB wireless documentation](https://android.googlesource.com/platform/packages/modules/adb/+/main/docs/dev/adb_wifi.md).

## Android 12 process limit

Android 12 phantom-process limits can stop background subprocesses. An affected
device may log `Trimming phantom processes`; battery exclusions alone may not
prevent it. The following example raises the limit to 256 and makes the setting
persist across bulk DeviceConfig resets. This changes remote system configuration
and some recovery defaults. Apply it only after reviewing the device's process
policy, using an **authorized ADB connection** from the computer:

```sh
adb shell getprop ro.build.version.sdk
adb shell device_config set_sync_disabled_for_tests persistent
adb shell device_config put activity_manager max_phantom_processes 256
adb shell device_config get activity_manager max_phantom_processes
adb shell settings get global device_config_sync_disabled
adb shell device_config is_sync_disabled_for_tests
```

For the example settings, verify the device API level and expect the process
limit and monitoring checks to return `256`, `1` and `true`.
A temporary test can use `until_reboot` instead of `persistent`, but
then production startup must be held after reboot until tuning is renewed.
Android 12's separate excessive-CPU policy remains in force.
Newer Android
versions have different controls; use the [Termux process-policy notes](https://github.com/agnostic-apollo/Android-Docs/blob/master/en/docs/apps/processes/phantom-cached-and-empty-processes.md#commands-to-disable-phantom-process-killing-and-tldr).
Before an Android OS update, stop production and re-enable sync:

```sh
adb shell device_config set_sync_disabled_for_tests none
```

After the update, verify the applicable process policy and reapply the approved
settings before starting production.
To return completely to the default limit,
also run `adb shell device_config delete activity_manager max_phantom_processes`.

## Debian and provider artifacts

Skip container creation on the already configured phone.
For a new prepared host,
use the current PRoot Distro install syntax from Termux:

```sh
proot-distro install debian:bookworm --name bifrost-debian
proot-distro login bifrost-debian
```

Inside Debian, keep the Linux toolchain ahead of Termux's native binaries:

```sh
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
apt-get update
apt-get install -y ca-certificates chromium ffmpeg python3 python3-venv build-essential curl
python3 -m venv /opt/bifrost-candidate/sticker-codec
/opt/bifrost-candidate/sticker-codec/bin/pip install Pillow==13.0.0
```

The launcher artifact layout is:

| Path inside bifrost-debian | Artifact |
| --- | --- |
| `/opt/bifrost-candidate/bin/bifrost` | Linux ARM64 Go executable |
| `/opt/bifrost-candidate/bin/node` | Linux ARM64 Node 22.23.4 |
| `/opt/bifrost-candidate/bin/telegram-bot-api` | Pinned local Bot API 10.4 executable |
| `/opt/bifrost-candidate/openwa` | Reviewed gateway build and ARM64 native Node dependencies |
| `/opt/bifrost-candidate/sticker-codec` | Pillow virtual environment |
| `/opt/bifrost-redis-glibc/8.10.3/bin` | Redis built with glibc and libc allocation |

Build Bifrost on the computer with the pinned Go toolchain:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o /tmp/bifrost-linux-arm64 ./cmd/bifrost
```

Copy reviewed ARM64 provider artifacts from their pinned builds.
OpenWA's
`dist`, `node_modules`, `dashboard/dist`, package files and scripts are runtime
artifacts; restore `/app/data` separately as private state.
Native `sharp` and
`better-sqlite3` modules must match the Linux ARM64 Node runtime.
Keep the gateway
commit pinned in `compose.yaml` and use `deploy/telegram-api.Dockerfile` for the
Telegram executable.
Do not float the gateway on an unreviewed branch.
Run the checked-in Redis build script inside Debian, binding the checkout from
Termux (replace the checkout path if needed):

```sh
proot-distro login --bind "$HOME/bifrost-src:/source" bifrost-debian -- /bin/bash /source/deploy/termux/build-redis-glibc.sh
```

The script verifies the source archive and uses `MALLOC=libc`. Some Android
ARM64 kernels trigger Redis's `ARM64-COW-BUG` warning; a different userspace build
does not repair the kernel. Review kernel compatibility and the implications of
any warning override. Preserve AOF persistence, loopback binding and noeviction.

PostgreSQL uses a separate `bifrost-postgres` container. When restoring an existing
database, match the source PostgreSQL version, libc and collation requirements.
Install a verified runtime archive without copying a running data directory:

```sh
proot-distro install /path/to/postgres-runtime-rootfs.tar.gz --name bifrost-postgres
```

Restore logical dumps and original roles into a fresh cluster.
Its launcher must
explicitly pass `--env PROOT_DONT_SHARE_LIBANDROID_SHMEM=1` to `proot-distro login`;
exporting that variable only in the parent shell does not reach the tracer and
can deadlock shared-memory initialization.
The checked-in launcher does this.
The Debian hosts file maps `bifrost`, `openwa`, `postgres`, `redis` and
`telegram-api` to `127.0.0.1`.
This preserves callback host ownership and the
existing service URLs.
The Node preload and Bifrost bind setting restrict native
listeners to loopback.
Configure the browser, ffmpeg and Python paths to match the installed artifacts.

## Restore state and install launchers

Quiesce the source runtime and paired providers first.
Preserve the permanent
account ID, encryption keys, bot ciphertexts, stable effect keys and unresolved
receipts.
Transfer both logical databases and roles, bridge media, OpenWA data,
Telegram data and Redis persistence.
Never start the same paired identity from
two hosts or reset the current delivery queue.
`deploy/termux/transfer-assets.py` packs or restores the four current asset roles
with manifests, path checks and independent content hashes.
For example, run on
frozen source files and a private destination:

```sh
python3 deploy/termux/transfer-assets.py pack media /media /private/checkpoint
python3 deploy/termux/transfer-assets.py restore media /private/checkpoint/media.tar.gz /private/checkpoint/media.manifest.json /new/media
```

The other roles are `openwa-data`, `telegram-data` and `redis`.
A restore refuses
an existing destination.
Keep token-directory names and manifests private.
The phone stores restored state under `~/bifrost-phone/state`.
Its private
`runtime.env.json` contains the literal service environments and permanent account
ID; do not shell-source or regenerate it.
Production requires independently
verified `CUTOVER-VERIFIED.json` and `ANDROID-PROCESS-CONTROL.json` receipts matching
the account, configuration hash and approved Android tuning.
The installer never
creates these proofs.
Preserve them with a verified checkpoint; never fabricate
verification flags just to bypass startup checks.
From the checkout in Termux, install the reusable launchers:

```sh
python deploy/termux/install.py
```

It installs service/log units and a wake-lock-first `~/.termux/boot/20-bifrost`.
New services are down by default.
Reinstalling preserves private state, activation
proofs and existing up/down choices.
Configure the existing Cloudflare connector
at `~/.cloudflared/token` with mode `0600` and its directory mode `0700` before
administrative boot.
The token is consumed from a file, not committed to Git.
Open Termux:Boot once, then use:

```sh
python ~/bifrost-phone/phone-boot.py
```

Administrative services have their own supervisor.
Production starts only after
verification.
On each reboot, unlock Android once so the boot receiver runs.
Supervisor readiness checks require live service and log control replies; stale
`supervise/ok` files do not establish readiness.

## Dashboard login and QR linking

On the phone, open `http://127.0.0.1:2785`.
On the computer with authorized ADB:

```sh
adb forward tcp:12785 tcp:2785
```

Open [localhost:12785](http://127.0.0.1:12785).
Obtain the dashboard key privately
from a **Termux SSH session**, not `adb shell`:

```sh
cat ~/bifrost-phone/state/openwa-data/.api-key
```

Paste the key only into the local dashboard.
Select the Bifrost session and use
its live QR page.
On the primary WhatsApp account, choose **Linked devices → Link
a device** and scan the latest QR.
The live page refreshes expired codes.
A new
session must use the same WhatsApp identity as the retained account; the bridge
checks native identity before forwarding.
The 300-second authentication timeout accommodates phone startup.
The relay's
`/readyz` becomes 200 only after native WhatsApp and Telegram setup completes.
The dashboard forward needs USB or wireless debugging; the relay itself does not.

## Services, logs and recovery

From Termux or its SSH session:

```sh
python ~/bifrost-phone/phone-control.py status
python ~/bifrost-phone/phone-control.py stop
python ~/bifrost-phone/phone-control.py start
```

Stop waits for the bridge and providers before Redis and PostgreSQL.
Rotating
logs are in `~/bifrost-phone/logs/ROLE/current`; inspect `openwa`, `bifrost`,
`postgres`, `redis`, `telegram-api`, `admin-sshd` or `admin-cloudflared`.
Boot results
are saved as `~/bifrost-phone/boot-last-result.json`.
Logs may contain private data.
Backups should include both databases, roles, assets, private configuration and
launchers, with checksums. For a host transfer, quiesce writers and use fresh
verified state. Keep retired providers stopped until their paired-session and
delivery state has been reconciled. Separate database locks do not coordinate
independent hosts.
Sticker lookups persist alongside content-addressed media.
Keep them in backups;
never remove immutable blobs referenced by frozen delivery plans.
See
[sticker limits and cache behavior](STICKER-LIMITS.md).

## References

[Termux installation](https://github.com/termux/termux-app#installation),
[Termux:Boot](https://github.com/termux/termux-boot),
[PRoot Distro](https://github.com/termux/proot-distro#readme), and
[ADB forwarding](https://android.googlesource.com/platform/packages/modules/adb/+/main/docs/dev/services.md).
