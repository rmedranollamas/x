#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "--- 🚀 X-Agent Cron Setup ---"
echo "Project Directory: ${PROJECT_DIR}"

# Check for .env file
ENV_FILE="${PROJECT_DIR}/.env"
if [[ ! -f "${ENV_FILE}" ]]; then
    echo "Warning: No .env file found in project root."
else
    if ! grep -q "REPORT_RECIPIENT" "${ENV_FILE}"; then
        echo "Warning: REPORT_RECIPIENT not found in .env. Email reporting might fail."
    fi
fi

# Ensure .state directory exists
mkdir -p "${PROJECT_DIR}/.state"

# Check binary existence
if [[ ! -x "${PROJECT_DIR}/x-agent" && ! -x "${PROJECT_DIR}/dist/x-agent" ]]; then
    echo "Warning: x-agent binary not found in project root or dist/. Run 'make build' first."
fi

CRON_SCHEDULE="0 9 * * *"
LOG_FILE="${PROJECT_DIR}/.state/cron.log"
CRON_COMMAND="cd ${PROJECT_DIR} && X_AGENT_ENV=production ./x-agent insights --email >> ${LOG_FILE} 2>&1"
CRON_LINE="${CRON_SCHEDULE} ${CRON_COMMAND}"

echo ""
echo "Proposed Crontab Line:"
echo -e "\033[96m${CRON_LINE}\033[0m"
echo ""

AUTO_CONFIRM=false
for arg in "$@"; do
    case "$arg" in
        -y|--yes)
            AUTO_CONFIRM=true
            ;;
    esac
done

if [[ "${AUTO_CONFIRM}" == "true" ]]; then
    CONFIRM="y"
else
    read -rp "Would you like to install this daily at 9:00 AM? (y/n): " CONFIRM
fi

if [[ "${CONFIRM}" =~ ^[Yy]$ ]]; then
    CURRENT_CRON="$(crontab -l 2>/dev/null || true)"

    if echo "${CURRENT_CRON}" | grep -Fq "${CRON_COMMAND}"; then
        echo "Job already exists in crontab. Skipping."
        exit 0
    fi

    if [[ -z "${CURRENT_CRON}" ]]; then
        NEW_CRON="${CRON_LINE}"
    else
        NEW_CRON="$(printf "%s\n%s" "${CURRENT_CRON}" "${CRON_LINE}")"
    fi

    echo "${NEW_CRON}" | crontab -

    echo ""
    echo "✅ Cronjob installed successfully!"
    echo "Logs will be available at: ${LOG_FILE}"
else
    echo "Setup cancelled."
fi
