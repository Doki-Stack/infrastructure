#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

echo -e "${RED}This will delete the kind cluster 'doki-stack'.${NC}"
echo -n "Are you sure? [y/N] "
read -r confirm
if [[ "$confirm" != [yY] && "$confirm" != [yY][eE][sS] ]]; then
  echo "Aborted."
  exit 0
fi

kind delete cluster --name doki-stack
echo -e "${GREEN}Cluster doki-stack deleted.${NC}"
