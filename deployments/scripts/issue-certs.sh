#!/bin/sh
set -eu

LEGO_IMAGE=${LEGO_IMAGE:-goacme/lego:v5.5.2}
LEGO_DATA=${LEGO_DATA:-/opt/artist-shop-secrets/acme}
TLS_DIR=${TLS_DIR:-/opt/artist-shop-tls}
VERCEL_TOKEN_FILE=${VERCEL_TOKEN_FILE:-/opt/artist-shop-secrets/vercel.token}
ACME_EMAIL=${ACME_EMAIL:-}
ACME_EMAIL_FILE=${ACME_EMAIL_FILE:-/opt/artist-shop-secrets/acme/account.email}
CERT_DOMAINS=${CERT_DOMAINS:-"api.sumichi.hycat.online"}
LEGO_RENEW_DAYS=${LEGO_RENEW_DAYS:-30}

if [ -z "$ACME_EMAIL" ] && [ -f "$ACME_EMAIL_FILE" ]; then
  ACME_EMAIL=$(cat "$ACME_EMAIL_FILE")
fi

if [ -z "$ACME_EMAIL" ]; then
  echo "ACME_EMAIL is required (Let's Encrypt account email)" >&2
  exit 1
fi

if [ ! -f "$VERCEL_TOKEN_FILE" ]; then
  echo "Vercel token file not found: $VERCEL_TOKEN_FILE" >&2
  exit 1
fi

VERCEL_API_TOKEN=$(cat "$VERCEL_TOKEN_FILE")
export VERCEL_API_TOKEN
if [ -n "${VERCEL_TEAM_ID:-}" ]; then
  export VERCEL_TEAM_ID
fi

# Certificates are installed into a stack-owned directory (no sudo, no
# dependency on any shared /etc/letsencrypt).
mkdir -p "$LEGO_DATA" "$TLS_DIR"

# lego v5 keeps all flags under the "run" subcommand and removes the standalone
# "renew" command: "lego run" gets or renews depending on --renew-days.
run_lego() {
  docker run --rm \
    --user "$(id -u):$(id -g)" \
    -v "${LEGO_DATA}:/data" \
    -e VERCEL_API_TOKEN \
    ${VERCEL_TEAM_ID:+-e VERCEL_TEAM_ID} \
    "${LEGO_IMAGE}" \
    run --email "$ACME_EMAIL" --dns vercel --accept-tos --path /data \
    --no-bundle --renew-days "$LEGO_RENEW_DAYS" "$@"
}

install_cert() {
  domain=$1
  cert_dir="$TLS_DIR/$domain"
  crt="$LEGO_DATA/certificates/$domain.crt"
  key="$LEGO_DATA/certificates/$domain.key"
  issuer="$LEGO_DATA/certificates/$domain.issuer.crt"

  if [ ! -f "$crt" ] || [ ! -f "$key" ]; then
    echo "missing lego output for $domain" >&2
    return 1
  fi

  mkdir -p "$cert_dir"
  if [ -f "$issuer" ]; then
    cat "$crt" "$issuer" > "$cert_dir/fullchain.pem"
  else
    cp "$crt" "$cert_dir/fullchain.pem"
  fi
  cp "$key" "$cert_dir/privkey.pem"
  chmod 644 "$cert_dir/fullchain.pem"
  chmod 600 "$cert_dir/privkey.pem"
  echo "installed certificate for $domain at $cert_dir"
}

ensure_cert() {
  domain=$1
  echo "ensuring certificate for $domain"
  run_lego --domains "$domain"
  install_cert "$domain"
}

command=${1:-issue}

case "$command" in
  issue|renew)
    for domain in $CERT_DOMAINS; do
      ensure_cert "$domain"
    done
    ;;
  *)
    echo "usage: $0 [issue|renew]" >&2
    exit 64
    ;;
esac

echo "certificate operation complete"
