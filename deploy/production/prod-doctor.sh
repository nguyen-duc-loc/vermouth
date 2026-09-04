#!/bin/sh
set -eu

prod_validate_azure_snapshot() {
  account_id=$1
  vm_resource_id=$2
  vm_document=$3
  nic_document=$4
  public_ip_document=$5
  effective_rules_document=$6

  subscription_id=$(printf '%s\n' "$vm_resource_id" | awk -F/ '
    NF == 9 && $2 == "subscriptions" && $4 == "resourceGroups" &&
      $6 == "providers" && tolower($7) == "microsoft.compute" &&
      tolower($8) == "virtualmachines" && $9 == "nguyenducloc-vm2" { print $3 }
  ')
  [ -n "$subscription_id" ] || prod_fail "PROD_AZURE_VM_RESOURCE_ID must be a complete virtual machine resource ID"
  [ "$account_id" = "$subscription_id" ] ||
    prod_fail "the active Azure subscription does not match PROD_AZURE_VM_RESOURCE_ID"

  jq -e \
    --arg id "$vm_resource_id" --arg name "$PROD_VM_NAME" --arg size "$PROD_VM_SIZE" --arg location "$PROD_LOCATION" '
      (.id | ascii_downcase) == ($id | ascii_downcase) and
      .name == $name and .hardwareProfile.vmSize == $size and .location == $location and
      (.networkProfile.networkInterfaces | length) == 1 and
      (.networkProfile.networkInterfaces[0].id | type == "string" and length > 0)
    ' "$vm_document" >/dev/null || prod_fail "the Azure VM does not match the committed production target"

  nic_id=$(jq -r '.networkProfile.networkInterfaces[0].id' "$vm_document")
  jq -e --arg id "$nic_id" '
    (.id | ascii_downcase) == ($id | ascii_downcase) and
    ([.ipConfigurations[] | .publicIPAddress.id // empty] | length) == 1
  ' "$nic_document" >/dev/null || prod_fail "the Azure VM must have one Public IP on its network interface"
  public_ip_id=$(jq -r '[.ipConfigurations[] | .publicIPAddress.id // empty][0]' "$nic_document")

  jq -e \
    --arg id "$public_ip_id" --arg address "$PROD_HOST" --arg label "$PROD_DNS_LABEL" --arg hostname "$PROD_HOSTNAME" '
      (.id | ascii_downcase) == ($id | ascii_downcase) and
      .publicIPAllocationMethod == "Static" and .ipAddress == $address and
      .dnsSettings.domainNameLabel == $label and .dnsSettings.fqdn == $hostname
    ' "$public_ip_document" >/dev/null ||
    prod_fail "the Azure Public IP, static allocation, or DNS label does not match production configuration"

  jq -e '
    [
      .. | objects |
      select((.direction? | strings | ascii_downcase) == "inbound") |
      select((.access? | strings | ascii_downcase) == "allow") |
      select(
        ([.sourceAddressPrefix? | select(. != null)] + (.sourceAddressPrefixes? // [])) |
        any(. == "Internet" or . == "internet" or . == "*" or . == "0.0.0.0/0")
      ) |
      ([.destinationPortRange? | select(. != null)] + (.destinationPortRanges? // []))[]
    ] | sort | unique == ["22", "443", "80"]
  ' "$effective_rules_document" >/dev/null ||
    prod_fail "effective Azure inbound rules must expose Internet traffic only on ports 22, 80, and 443"
}

prod_check_azure_control_plane() {
  prod_require_env PROD_AZURE_VM_RESOURCE_ID
  printf '%s' "$PROD_AZURE_VM_RESOURCE_ID" | grep -Eq '/virtualMachines/[^/]+$' ||
    prod_fail "PROD_AZURE_VM_RESOURCE_ID must identify one virtual machine"
  PROD_WORK_DIR=$(mktemp -d "$PROD_TMP/doctor.XXXXXX")
  export PROD_WORK_DIR
  trap prod_cleanup EXIT HUP INT TERM

  account_id=$(az account show --query id --output tsv)
  az vm show --ids "$PROD_AZURE_VM_RESOURCE_ID" --output json >"$PROD_WORK_DIR/vm.json"
  nic_id=$(jq -r '.networkProfile.networkInterfaces[0].id // empty' "$PROD_WORK_DIR/vm.json")
  [ -n "$nic_id" ] || prod_fail "the Azure VM has no network interface"
  az network nic show --ids "$nic_id" --output json >"$PROD_WORK_DIR/nic.json"
  public_ip_id=$(jq -r '[.ipConfigurations[] | .publicIPAddress.id // empty][0] // empty' "$PROD_WORK_DIR/nic.json")
  [ -n "$public_ip_id" ] || prod_fail "the Azure VM has no Public IP"
  az network public-ip show --ids "$public_ip_id" --output json >"$PROD_WORK_DIR/public-ip.json"
  az network nic list-effective-nsg --ids "$nic_id" --output json >"$PROD_WORK_DIR/effective-nsg.json"
  prod_validate_azure_snapshot \
    "$account_id" "$PROD_AZURE_VM_RESOURCE_ID" "$PROD_WORK_DIR/vm.json" "$PROD_WORK_DIR/nic.json" \
    "$PROD_WORK_DIR/public-ip.json" "$PROD_WORK_DIR/effective-nsg.json"
  printf '%-18s %s\n' Azure "$PROD_VM_NAME, static $PROD_HOST, ports 22, 80, and 443 only"
}

prod_probe_ports() {
  for port in $(prod_config_value network.public_ports | jq -r '.[]'); do
    nc -z -w 5 "$PROD_HOST" "$port" ||
      prod_fail "public TCP port $port is not reachable. Check the Azure network rule and Traefik."
    printf '%-18s %s\n' "public:$port" reachable
  done
  for port in $(prod_config_value network.blocked_ports | jq -r '.[]'); do
    if nc -z -w 2 "$PROD_HOST" "$port"; then
      prod_fail "TCP port $port is publicly reachable and must be blocked"
    fi
    printf '%-18s %s\n' "blocked:$port" closed
  done
}

prod_doctor() {
  PROD_FAILURE_CODE=2
  prod_init
  for tool in az dig nc; do
    prod_need "$tool"
  done
  mkdir -p -m 0700 "$PROD_TMP"
  chmod 0700 "$PROD_TMP"

  committed_proxy=$(prod_platform_config get "$PROD_VALUES" config.gatewayTrustedProxyCIDRs)
  [ "$committed_proxy" = "$PROD_POD_CIDR" ] ||
    prod_fail "production trusted proxy CIDRs must equal the committed k3s pod CIDR $PROD_POD_CIDR"
  prod_check_azure_control_plane

  addresses=$(dig +short A "$PROD_HOSTNAME" | LC_ALL=C sort -u)
  [ "$addresses" = "$PROD_HOST" ] ||
    prod_fail "$PROD_HOSTNAME resolves to ${addresses:-nothing}, expected $PROD_HOST. Configure the Azure Public IP DNS label first."
  printf '%-18s %s\n' DNS "$PROD_HOSTNAME to $PROD_HOST"

  remote_environment=$(prod_remote_environment)
  prod_ssh "sudo --non-interactive env $remote_environment /bin/sh -s" <"$PROD_DIR/remote-doctor.sh"
  printf '%-18s %s\n' SSH "strict host checking accepted $PROD_HOST"

  prod_probe_ports

  printf '%s\n' "Production doctor passed for https://$PROD_HOSTNAME"
}
