#!/bin/sh
# Install reclaim.
#
# Prefers a native package where one fits the machine, because a package can be
# upgraded and removed by the tools that already manage everything else there.
# Falls back to a plain binary in /usr/local/bin when it cannot.
#
#   curl -fsSL https://raw.githubusercontent.com/mralaminahamed/reclaim/trunk/install.sh | sh
#
# Environment:
#   RECLAIM_VERSION   version to install (default: latest release)
#   RECLAIM_BINDIR    where the fallback binary goes (default: /usr/local/bin)
#   RECLAIM_METHOD    force "package" or "binary"

set -eu

REPO=mralaminahamed/reclaim
BINDIR=${RECLAIM_BINDIR:-/usr/local/bin}
METHOD=${RECLAIM_METHOD:-auto}
TMP=

cleanup() { [ -n "$TMP" ] && rm -rf "$TMP"; }
trap cleanup EXIT INT TERM

die() { echo "install: $*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# sudo only when not already root, so this works inside a container image build
# where there is no sudo at all.
as_root() {
    if [ "$(id -u)" -eq 0 ]; then "$@"; else
        have sudo || die "need root to install; re-run as root or install sudo"
        sudo "$@"
    fi
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo amd64 ;;
        aarch64|arm64)  echo arm64 ;;
        *) die "unsupported architecture $(uname -m); build from source with: go install github.com/$REPO/cmd/reclaim@latest" ;;
    esac
}

# The package format a machine expects, not the distribution's name. Deriving
# from the tool that is present is what makes this work on the derivatives too,
# and there are far more of those than there are upstreams.
detect_format() {
    if have dpkg-deb || have apt-get; then echo deb
    elif have rpm || have dnf || have yum || have zypper; then echo rpm
    else echo none
    fi
}

fetch() {
    if have curl; then curl -fsSL "$1" -o "$2"
    elif have wget; then wget -qO "$2" "$1"
    else die "need curl or wget"
    fi
}

latest_version() {
    TAGS_URL="https://api.github.com/repos/$REPO/releases/latest"
    fetch "$TAGS_URL" "$TMP/release.json"
    # Deliberately not jq: this script has to run before anything is installed.
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$TMP/release.json" \
        | head -1
}

verify() {
    # A checksum is only worth anything if a missing one is a failure rather
    # than a shrug.
    file=$1
    have sha256sum || { echo "install: sha256sum not found, skipping verification" >&2; return 0; }
    fetch "https://github.com/$REPO/releases/download/v$VERSION/SHA256SUMS" "$TMP/SHA256SUMS" \
        || die "no checksum file published for v$VERSION"
    ( cd "$TMP" && grep " $(basename "$file")\$" SHA256SUMS | sha256sum -c - >/dev/null ) \
        || die "checksum mismatch for $(basename "$file")"
}

TMP=$(mktemp -d)
ARCH=$(detect_arch)
VERSION=${RECLAIM_VERSION:-}
[ -n "$VERSION" ] || VERSION=$(latest_version)
[ -n "$VERSION" ] || die "could not determine the latest version; set RECLAIM_VERSION"
VERSION=${VERSION#v}

FORMAT=none
[ "$METHOD" = binary ] || FORMAT=$(detect_format)

case "$FORMAT" in
    deb)
        PKG="reclaim_${VERSION}_${ARCH}.deb"
        echo "installing reclaim $VERSION as a .deb"
        fetch "https://github.com/$REPO/releases/download/v$VERSION/$PKG" "$TMP/$PKG" \
            || die "no .deb published for $ARCH at v$VERSION"
        verify "$TMP/$PKG"
        as_root dpkg -i "$TMP/$PKG"
        ;;
    rpm)
        PKG="reclaim-${VERSION}-1.x86_64.rpm"
        [ "$ARCH" = amd64 ] || die "no .rpm published for $ARCH; re-run with RECLAIM_METHOD=binary"
        echo "installing reclaim $VERSION as an .rpm"
        fetch "https://github.com/$REPO/releases/download/v$VERSION/$PKG" "$TMP/$PKG" \
            || die "no .rpm published at v$VERSION"
        verify "$TMP/$PKG"
        if have dnf; then as_root dnf install -y "$TMP/$PKG"
        elif have zypper; then as_root zypper --non-interactive install "$TMP/$PKG"
        elif have yum; then as_root yum install -y "$TMP/$PKG"
        else as_root rpm -Uvh "$TMP/$PKG"
        fi
        ;;
    *)
        TARBALL="reclaim_${VERSION}_linux_${ARCH}.tar.gz"
        echo "installing reclaim $VERSION to $BINDIR"
        fetch "https://github.com/$REPO/releases/download/v$VERSION/$TARBALL" "$TMP/$TARBALL" \
            || die "no build published for linux/$ARCH at v$VERSION"
        verify "$TMP/$TARBALL"
        tar -C "$TMP" -xzf "$TMP/$TARBALL"
        as_root install -Dm0755 "$TMP/reclaim" "$BINDIR/reclaim"
        ;;
esac

echo
"$(command -v reclaim || echo "$BINDIR/reclaim")" version
echo
echo "Next:  reclaim clean          # measure, delete nothing"
echo "       reclaim clean --apply  # reclaim, after confirming"
