#!/usr/bin/env bash
# Install or upgrade Meridian on a Debian-family box.
#
# The installer is deliberately thin: it creates the layout, installs the host binary and the
# root-owned allowlist, asks meridian-host to render the unit templates, validates them with the
# system's own tools, and then verifies that the probe and the queue watcher are alive. The digest
# for the first image is resolved from GHCR unless --image-digest says it, so .env starts pinned.
#
# Run it from a checkout of this repo: the templates and examples are read from ./deploy.
# It refuses to run as a non-root user; use sudo. --dry-run prints every action and changes nothing.

set -euo pipefail
umask 022

readonly IMAGE_REPO="ghcr.io/autumnsgrove/meridian"
readonly HOST_BIN="/etc/meridian/meridian-host"
readonly ETCDIR="/etc/meridian"
readonly SYSTEMD_DIR="/etc/systemd/system"
readonly SUDOERS_FILE="/etc/sudoers.d/meridian"

deploy_user="${SUDO_USER:-}"
deploy_dir=""
host_binary=""
version=""
image_digest=""
image_tag=""
upgrade_host=0
dry_run=0
tmp=""
render_tmp=""

usage() {
	cat <<'EOF'
usage: install.sh [flags]

  --deploy-user NAME     the account the container and the units run as (default: $SUDO_USER)
  --dir PATH             the Meridian data directory (default: ~NAME/Meridian)
  --host-binary PATH     install this meridian-host binary
  --version vX.Y.Z       download meridian-host and SHA256SUMS from that GitHub release
  --image-digest DIGEST  pin the first image to this sha256: digest
  --image-tag TAG        resolve the digest for TAG (default: latest, or --version)
  --upgrade-host         replace only /etc/meridian/meridian-host, then exit
  --dry-run              print what would happen and change nothing
  -h, --help             show this help

One of --host-binary or --version is required. Run it with sudo from the repo's deploy/ directory.
EOF
}

die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

note() {
	printf '%s\n' "$*"
}

say() {
	if [ "$dry_run" = 1 ]; then
		printf 'would: %s\n' "$*"
	else
		printf '==> %s\n' "$*"
	fi
}

cleanup() {
	if [ -n "${tmp:-}" ]; then
		rm -rf "$tmp"
	fi
	if [ -n "${render_tmp:-}" ]; then
		rm -rf "$render_tmp"
	fi
}
trap cleanup EXIT

need_value() {
	[ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 needs a value"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--deploy-user)
		need_value "$1" "${2:-}"
		deploy_user=$2
		shift 2
		;;
	--deploy-user=*)
		deploy_user=${1#*=}
		shift
		;;
	--dir)
		need_value "$1" "${2:-}"
		deploy_dir=$2
		shift 2
		;;
	--dir=*)
		deploy_dir=${1#*=}
		shift
		;;
	--host-binary)
		need_value "$1" "${2:-}"
		host_binary=$2
		shift 2
		;;
	--host-binary=*)
		host_binary=${1#*=}
		shift
		;;
	--version)
		need_value "$1" "${2:-}"
		version=$2
		shift 2
		;;
	--version=*)
		version=${1#*=}
		shift
		;;
	--image-digest)
		need_value "$1" "${2:-}"
		image_digest=$2
		shift 2
		;;
	--image-digest=*)
		image_digest=${1#*=}
		shift
		;;
	--image-tag)
		need_value "$1" "${2:-}"
		image_tag=$2
		shift 2
		;;
	--image-tag=*)
		image_tag=${1#*=}
		shift
		;;
	--upgrade-host)
		upgrade_host=1
		shift
		;;
	--dry-run)
		dry_run=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "unknown argument $1 (try --help)"
		;;
	esac
done

if [ "$dry_run" = 0 ] && [ "$(id -u)" -ne 0 ]; then
	die "run this with sudo; it writes /etc/meridian, /etc/systemd/system and /etc/sudoers.d"
fi
if [ -z "$deploy_user" ]; then
	die "--deploy-user is required when SUDO_USER is empty"
fi
[ "$deploy_user" != "root" ] || die "the deploy user must not be root"
id -u "$deploy_user" >/dev/null 2>&1 || die "no such user: $deploy_user"
deploy_group=$(id -gn "$deploy_user")
deploy_uid=$(id -u "$deploy_user")
deploy_gid=$(id -g "$deploy_user")

if [ -n "$host_binary" ] && [ -n "$version" ]; then
	die "pass one of --host-binary or --version, not both"
fi
if [ -z "$host_binary" ] && [ -z "$version" ]; then
	die "one of --host-binary or --version is required"
fi
if [ -n "$version" ]; then
	case "$version" in
	v[0-9]*) ;;
	*) die "--version must be a release tag like v0.1.0" ;;
	esac
	case "$version" in
	*[!A-Za-z0-9._-]*) die "--version contains an unsafe character" ;;
	esac
fi
if [ -n "$image_digest" ]; then
	# The same grammar the updater and the host side use: sha256 and 64 lowercase hex characters.
	[[ "$image_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || die "--image-digest must be sha256:<64 lowercase hex>"
fi
if [ -z "$image_tag" ]; then
	if [ -n "$version" ]; then
		image_tag=$version
	else
		image_tag=latest
	fi
fi

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
[ -f "$repo_root/deploy/docker-compose.yml" ] || die "run install.sh from the repo's deploy/ directory"
[ -f "$repo_root/deploy/systemd/meridian-control.path" ] || die "$repo_root/deploy does not have the unit templates"

if [ -z "$deploy_dir" ]; then
	if command -v getent >/dev/null 2>&1; then
		deploy_home=$(getent passwd "$deploy_user" | cut -d: -f6)
	else
		# shellcheck disable=SC2086 # the tilde is the point: expand the named user's home
		deploy_home=$(eval echo "~$deploy_user")
	fi
	[ -n "$deploy_home" ] || die "could not work out $deploy_user's home; pass --dir"
	deploy_dir="$deploy_home/Meridian"
fi
case "$deploy_dir" in
/*) ;;
*) die "--dir must be an absolute path" ;;
esac
case "$deploy_dir" in
*[\"\\]*) die "--dir must not contain a quote or backslash" ;;
*$'\n'* | *$'\r'*) die "--dir must not contain a newline" ;;
esac
control_dir="$deploy_dir/control"
status_dir="$deploy_dir/status"

if [ -n "$host_binary" ]; then
	[ -f "$host_binary" ] || die "no such binary: $host_binary"
	case "$host_binary" in
	/*) host_source=$host_binary ;;
	*) host_source="$(CDPATH='' cd -- "$(dirname -- "$host_binary")" && pwd)/$(basename -- "$host_binary")" ;;
	esac
else
	case "$(uname -m)" in
	aarch64 | arm64) arch=arm64 ;;
	x86_64 | amd64) arch=amd64 ;;
	*) die "no meridian-host build for $(uname -m)" ;;
	esac
	release_url="https://github.com/autumnsgrove/meridian/releases/download/${version}"
	host_source="$release_url/meridian-host-linux-${arch}"
fi

# resolve_digest asks GHCR for the manifest digest behind a tag. It is only a starting pin: the
# container's own update path resolves and re-validates every later one.
resolve_digest() {
	local repo=$1 tag=$2 token digest
	token=$(curl -fsSL "https://ghcr.io/token?service=ghcr.io&scope=repository:${repo}:pull" |
		sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
	[ -n "$token" ] || return 1
	digest=$(curl -fsSI \
		-H "Authorization: Bearer ${token}" \
		-H "Accept: application/vnd.oci.image.index.v1+json" \
		-H "Accept: application/vnd.docker.distribution.manifest.list.v2+json" \
		"https://ghcr.io/v2/${repo}/manifests/${tag}" |
		tr -d '\r' | sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: //p')
	printf '%s\n' "$digest"
}

create_dir() {
	local path=$1 owner=$2 group=$3 mode=$4
	say "create $path (owner $owner:$group, mode $mode)"
	if [ "$dry_run" = 0 ]; then
		install -d -o "$owner" -g "$group" -m "$mode" "$path"
	fi
}

install_file() {
	local source=$1 target=$2 owner=$3 group=$4 mode=$5
	if [ -e "$target" ]; then
		note "leave existing $target"
		return 0
	fi
	say "install $source -> $target (owner $owner:$group, mode $mode)"
	if [ "$dry_run" = 0 ]; then
		install -o "$owner" -g "$group" -m "$mode" "$source" "$target"
	fi
}

say "install the host binary at $HOST_BIN"
if [ "$dry_run" = 0 ]; then
	if [ -n "$version" ]; then
		tmp=$(mktemp -d)
		say "download $host_source and SHA256SUMS"
		curl -fsSL -o "$tmp/SHA256SUMS" "${host_source%/*}/SHA256SUMS"
		curl -fsSL -o "$tmp/meridian-host-linux-${arch}" "$host_source"
		grep " meridian-host-linux-${arch}\$" "$tmp/SHA256SUMS" >"$tmp/expected"
		[ -s "$tmp/expected" ] || die "SHA256SUMS has no entry for meridian-host-linux-${arch}"
		(cd "$tmp" && sha256sum -c expected)
		host_source="$tmp/meridian-host-linux-${arch}"
	fi
	install -d -o root -g root -m 0755 "$ETCDIR"
	install -o root -g root -m 0755 "$host_source" "$HOST_BIN"
else
	say "install $host_source -> $HOST_BIN (owner root:root, mode 0755)"
fi

if [ "$upgrade_host" = 1 ]; then
	note "host binary upgraded; the units, allowlist, .env and containers were left alone"
	exit 0
fi

create_dir "$deploy_dir" "$deploy_user" "$deploy_group" 0755
create_dir "$deploy_dir/config" "$deploy_user" "$deploy_group" 0755
create_dir "$deploy_dir/icons" "$deploy_user" "$deploy_group" 0755
create_dir "$status_dir" "$deploy_user" "$deploy_group" 0755
create_dir "$control_dir" "$deploy_user" "$deploy_group" 0755
create_dir "$control_dir/inbox" "$deploy_user" "$deploy_group" 0755
create_dir "$control_dir/inbox/tmp" "$deploy_user" "$deploy_group" 0755
create_dir "$control_dir/inbox/queue" "$deploy_user" "$deploy_group" 0755
create_dir "$control_dir/outbox" root root 0755
create_dir "$control_dir/outbox/results" root root 0755
create_dir "$deploy_dir/update-signal" "$deploy_user" "$deploy_group" 0755

install_file "$repo_root/deploy/docker-compose.yml" "$deploy_dir/docker-compose.yml" \
	"$deploy_user" "$deploy_group" 0644
if [ -e "$deploy_dir/docker-compose.yml" ] && ! grep -q 'update-signal:/update-signal' "$deploy_dir/docker-compose.yml"; then
	note "warning: $deploy_dir/docker-compose.yml does not mount ./update-signal; add it before updating"
fi
install_file "$repo_root/config.yaml.example" "$deploy_dir/config.yaml" \
	"$deploy_user" "$deploy_group" 0644

if [ -e "$deploy_dir/.env" ]; then
	note "leave existing $deploy_dir/.env"
else
	if [ -z "$image_digest" ]; then
		if [ "$dry_run" = 1 ]; then
			image_digest="sha256:<resolved from ghcr.io/autumnsgrove/meridian:${image_tag}>"
		else
			resolved=$(resolve_digest "${IMAGE_REPO#ghcr.io/}" "$image_tag") ||
				die "could not resolve ${IMAGE_REPO}:${image_tag}; pass --image-digest"
			[[ "$resolved" =~ ^sha256:[0-9a-f]{64}$ ]] ||
				die "GHCR returned a digest this installer will not write: $resolved"
			image_digest=$resolved
		fi
	fi
	say "write $deploy_dir/.env with the image pinned to $IMAGE_REPO@$image_digest"
	if [ "$dry_run" = 0 ]; then
		umask 077
		printf 'MERIDIAN_IMAGE=%s@%s\nMERIDIAN_UID=%s\nMERIDIAN_GID=%s\n' \
			"$IMAGE_REPO" "$image_digest" "$deploy_uid" "$deploy_gid" >"$deploy_dir/.env"
		umask 022
		chown "$deploy_user:$deploy_group" "$deploy_dir/.env"
		chmod 0600 "$deploy_dir/.env"
	fi
fi

say "write $ETCDIR/host.json"
if [ "$dry_run" = 0 ]; then
	printf '{"control_dir":"%s","deploy_user":"%s"}\n' "$control_dir" "$deploy_user" >"$ETCDIR/host.json"
	chown root:root "$ETCDIR/host.json"
	chmod 0644 "$ETCDIR/host.json"
fi
install_file "$repo_root/deploy/targets.example.json" "$ETCDIR/targets.json" root root 0644
install_file "$repo_root/deploy/observe.example.json" "$ETCDIR/observe.json" root root 0644
if [ -e "$ETCDIR/observe.json" ] && ! grep -q 'meridian-control.path' "$ETCDIR/observe.json"; then
	note "warning: $ETCDIR/observe.json does not watch meridian-control.path; add it"
fi
if [ -e "$ETCDIR/targets.json" ]; then
	note "the allowlist is yours: edit $ETCDIR/targets.json before using the control page"
fi

say "render and install the systemd units and the sudoers rule"
if [ "$dry_run" = 0 ]; then
	render_tmp=$(mktemp -d)
	"$HOST_BIN" render-units \
		--templates "$repo_root/deploy" \
		--output "$render_tmp" \
		--control-dir "$control_dir" \
		--deploy-user "$deploy_user" \
		--status-dir "$status_dir"
	systemd-analyze verify "$render_tmp"/systemd/*.service "$render_tmp"/systemd/*.timer "$render_tmp"/systemd/*.path ||
		die "the rendered units did not validate"
	visudo -cf "$render_tmp/sudoers/meridian" >/dev/null || die "the rendered sudoers file did not validate"
	for unit in "$render_tmp"/systemd/*; do
		install -o root -g root -m 0644 "$unit" "$SYSTEMD_DIR/$(basename -- "$unit")"
	done
	install -o root -g root -m 0440 "$render_tmp/sudoers/meridian" "$SUDOERS_FILE"
	systemctl daemon-reload
else
	say "validate them with systemd-analyze verify and visudo -cf"
	say "install them into $SYSTEMD_DIR and $SUDOERS_FILE"
fi

say "enable the probe timer and the control path"
if [ "$dry_run" = 0 ]; then
	systemctl enable --now meridian-probe.timer meridian-control.path
fi

failures=0
report() {
	local ok=$1 label=$2
	if [ "$ok" = 1 ]; then
		printf '  ok    %s\n' "$label"
	else
		printf '  FAIL  %s\n' "$label"
		failures=$((failures + 1))
	fi
}

if [ "$dry_run" = 1 ]; then
	note "would verify: meridian-probe.timer active and enabled, meridian-control.path active and enabled,"
	note "would verify: neither unit failed, and $status_dir/host.json appears within 45s"
else
	active=0
	systemctl is-active --quiet meridian-probe.timer && active=1
	report "$active" "meridian-probe.timer is active"

	enabled=0
	systemctl is-enabled --quiet meridian-probe.timer && enabled=1
	report "$enabled" "meridian-probe.timer is enabled"

	path_active=0
	systemctl is-active --quiet meridian-control.path && path_active=1
	report "$path_active" "meridian-control.path is active"

	path_enabled=0
	systemctl is-enabled --quiet meridian-control.path && path_enabled=1
	report "$path_enabled" "meridian-control.path is enabled"

	failed=0
	[ "$(systemctl is-failed meridian-probe.timer 2>/dev/null || true)" = "failed" ] && failed=1
	[ "$(systemctl is-failed meridian-control.path 2>/dev/null || true)" = "failed" ] && failed=1
	report "$((1 - failed))" "neither unit is failed"

	probe=0
	for _ in $(seq 1 45); do
		if [ -s "$status_dir/host.json" ]; then
			probe=1
			break
		fi
		sleep 1
	done
	report "$probe" "the probe wrote $status_dir/host.json"

	if [ "$failures" -ne 0 ]; then
		die "$failures check(s) failed; read journalctl -u meridian-probe.service and -u meridian-control.path"
	fi
fi

note ""
note "If this box is on a tailnet, expose the dashboard with:"
note "  tailscale serve --bg --https=8891 http://127.0.0.1:8891"
note "The installer does not run it for you."
