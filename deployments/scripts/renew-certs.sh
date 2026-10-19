#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

sh "${script_dir}/issue-certs.sh" renew

reload_nginx() {
  dir=$1
  compose_file=$2
  env_file=$3
  deploy_file=$4

  if [ ! -f "${dir}/${compose_file}" ]; then
    return 0
  fi

  echo "reloading nginx in ${dir}"
  (cd "${dir}" && docker compose \
    --env-file "${env_file}" \
    --env-file "${deploy_file}" \
    -f "${compose_file}" \
    exec -T nginx nginx -s reload) || true
}

reload_nginx /opt/artist-shop docker-compose.prod.yml .env.production .env.deploy

echo "certificate renewal complete"
