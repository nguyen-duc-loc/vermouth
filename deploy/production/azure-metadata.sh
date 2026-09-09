#!/bin/sh

azure_public_ip() {
  instance_metadata=$1
  public_ip=$(printf '%s' "$instance_metadata" |
    jq -r '[.network.interface[].ipv4.ipAddress[].publicIpAddress | select(length > 0)][0] // empty')
  if [ -n "$public_ip" ]; then
    printf '%s\n' "$public_ip"
    return
  fi

  load_balancer_metadata=$(curl -fsS --max-time 5 -H Metadata:true --noproxy '*' \
    'http://169.254.169.254/metadata/loadbalancer?api-version=2020-10-01') || return 1
  printf '%s' "$load_balancer_metadata" |
    jq -r '[.loadbalancer.publicIpAddresses[].frontendIpAddress | select(length > 0)][0] // empty'
}
