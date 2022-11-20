#!/usr/bin/env bash

set -eo pipefail

# Find and change to repo root directory
OS=$(uname)
if [[ "$OS" == "Darwin" ]]; then
	# OSX uses BSD readlink
	BASEDIR="$(dirname "$0")"
else
	BASEDIR=$(readlink -e "$(dirname "$0")")
fi
cd "${BASEDIR}/../.." || exit 1

echo '#########################################################################'
echo '## NOTE: A prerequisite for running this command locally is to have    ##'
echo '## running instances of mock_uss acting as RID SP, RID DP, and SCD     ##'
echo '## (../mock_uss/run_locally_ridsp.sh) and                              ##'
echo '## (../mock_uss/run_locally_riddp.sh) and                              ##'
echo '## (../mock_uss/run_locally_scdsc.sh) including related dependencies.  ##'
echo '#########################################################################'

monitoring/build.sh || exit 1

CONFIG_NAME="${1:-configurations.dev.local_test}"
CONFIG_FLAG="--config ${CONFIG_NAME}"

AUTH_SPEC='DummyOAuth(http://host.docker.internal:8085/token,uss_qualifier)'

QUALIFIER_OPTIONS="$CONFIG_FLAG"

REPORT_FILE="$(pwd)/monitoring/uss_qualifier/report.json"
# Report file must already exist to share correctly with the Docker container
touch "${REPORT_FILE}"

TESTED_REQS_FILE="$(pwd)/monitoring/uss_qualifier/tested_requirements.html"
touch "${TESTED_REQS_FILE}"

DOT_FILE="$(pwd)/monitoring/uss_qualifier/report.gv"
touch "${DOT_FILE}"

if [ "$CI" == "true" ]; then
  docker_args="--add-host host.docker.internal:host-gateway" # Required to reach other containers in Ubuntu (used for Github Actions)
else
  docker_args="-it"
fi

# shellcheck disable=SC2086
docker run ${docker_args} --name uss_qualifier \
  --rm \
  -e QUALIFIER_OPTIONS="${QUALIFIER_OPTIONS}" \
  -e PYTHONBUFFERED=1 \
  -e AUTH_SPEC=${AUTH_SPEC} \
  -v "${REPORT_FILE}:/app/monitoring/uss_qualifier/report.json" \
  -v "${TESTED_REQS_FILE}:/app/monitoring/uss_qualifier/tested_requirements.html" \
  -v "${DOT_FILE}:/app/monitoring/uss_qualifier/report.gv" \
  -v "$(pwd):/app" \
  -w /app/monitoring/uss_qualifier \
  interuss/monitoring \
  python main.py $QUALIFIER_OPTIONS \
  --report report.json \
  --tested_requirements tested_requirements.html \
  --role_requirements uss1,uss2=astm.f3548.v21.scd \
  --role_requirements uss1=astm.f3411.v19.service_provider \
  --role_requirements uss2=astm.f3411.v19.display_provider \
  --dot report.gv
