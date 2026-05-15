#!/usr/bin/env bash
set -e
if [ "${DEBUG_ENTRYPOINT}" = "true" ]; then
  echo "[DOCKER ENTRYPOINT] - DEBUG_ENTRYPOINT detected, all commands will be printed"
  set -x
fi


echo "[DOCKER ENTRYPOINT] - starting entrypoint .."

  if [ -e /bin/wait4x ];
  then
    exec /bin/wait4x
  fi

# if [ $# -gt 0 ]; then
#   if [ -f ./${1} ]; then
#     exec "./${@}"
#   elif [ -f ${1} ]; then
#     exec "${@}"
#   fi
# else
#  exec "/usr/bin/probe-service"
# fi

exec "/usr/bin/probe-service"

# wait for 30 seconds after finishing entrypoint scripts to prevent any service from starting a restart loop
# on startup failures, which would result in unneccessary high load
echo "[DOCKER ENTRYPOINT] - waiting for 30 seconds before killing the container"
sleep 30
