# NTP Time Synchronization Validation

This document describes how to validate NTP/chrony time synchronization on hosts running Ruby Core services, as required by ADR-0026.

## Why This Matters

Ruby Core uses time-sensitive logic including:

- Command TTL validation (commands expire after a configurable window)
- Time-based automations (debounce, delays)
- State drift reconciliation with timestamps
- Audit log timestamps

Clock skew between services can cause non-deterministic failures. While the application includes a configurable tolerance window (default 1000ms per ADR-0026), infrastructure-level time sync is the primary defense.

## Pre-Deployment Checklist

Before running Ruby Core services in production, verify:

- [ ] NTP daemon is installed and running
- [ ] NTP is synchronized to a reliable time source
- [ ] Clock offset is within acceptable bounds (< 100ms recommended)
- [ ] NTP service is enabled to start on boot

## Validation Commands

### Using timedatectl (systemd)

Most modern Linux distributions use systemd-timesyncd:

```bash
# Check overall time sync status
timedatectl status

# Expected output should show:
#   System clock synchronized: yes
#   NTP service: active
```

Example good output:

```
               Local time: Mon 2026-02-02 10:00:00 UTC
           Universal time: Mon 2026-02-02 10:00:00 UTC
                 RTC time: Mon 2026-02-02 10:00:00
                Time zone: UTC (UTC, +0000)
System clock synchronized: yes
              NTP service: active
          RTC in local TZ: no
```

### Using chronyc (chrony)

If using chrony (common on RHEL/CentOS/Fedora):

```bash
# Check tracking status
chronyc tracking

# Key fields to verify:
#   Leap status: Normal
#   System time: offset should be small (milliseconds)

# Check source status
chronyc sources -v

# Verify at least one source shows '*' (selected) or '+' (candidate)
```

Example good output:

```
Reference ID    : A29FC87B (time.cloudflare.com)
Stratum         : 3
Ref time (UTC)  : Mon Feb 02 10:00:00 2026
System time     : 0.000012345 seconds fast of NTP time
Last offset     : +0.000001234 seconds
RMS offset      : 0.000012345 seconds
Frequency       : 1.234 ppm slow
Residual freq   : +0.001 ppm
Skew            : 0.012 ppm
Root delay      : 0.012345678 seconds
Root dispersion : 0.001234567 seconds
Update interval : 64.0 seconds
Leap status     : Normal
```

### Using ntpq (ntpd)

If using traditional ntpd:

```bash
# Check peer status
ntpq -p

# Verify at least one peer shows '*' (sys.peer)

# Check sync status
ntpstat
```

## Automated Validation Script

Save this script as `check-ntp.sh` for automated validation:

```bash
#!/bin/bash
# NTP validation script for Ruby Core deployment

set -e

echo "=== NTP Validation for Ruby Core ==="
echo ""

# Check if time is synchronized
if command -v timedatectl &> /dev/null; then
    SYNCED=$(timedatectl show --property=NTPSynchronized --value)
    if [ "$SYNCED" = "yes" ]; then
        echo "[OK] System clock is synchronized (timedatectl)"
    else
        echo "[FAIL] System clock is NOT synchronized"
        echo "       Run: timedatectl set-ntp true"
        exit 1
    fi
elif command -v chronyc &> /dev/null; then
    if chronyc tracking | grep -q "Leap status.*Normal"; then
        echo "[OK] System clock is synchronized (chrony)"
    else
        echo "[FAIL] Chrony reports abnormal leap status"
        exit 1
    fi
elif command -v ntpstat &> /dev/null; then
    if ntpstat &> /dev/null; then
        echo "[OK] System clock is synchronized (ntpd)"
    else
        echo "[FAIL] ntpstat reports clock not synchronized"
        exit 1
    fi
else
    echo "[WARN] No NTP client detected (timedatectl, chronyc, or ntpstat)"
    echo "       Install and configure an NTP client before production deployment"
    exit 1
fi

# Check offset if possible
if command -v chronyc &> /dev/null; then
    OFFSET=$(chronyc tracking | grep "System time" | awk '{print $4}')
    echo "[INFO] Current offset: ${OFFSET} seconds"
fi

echo ""
echo "=== Validation Complete ==="
```

## Troubleshooting

### "System clock synchronized: no"

Enable NTP synchronization:

```bash
# For systemd-timesyncd
sudo timedatectl set-ntp true

# For chrony
sudo systemctl enable --now chronyd

# For ntpd
sudo systemctl enable --now ntpd
```

### Large clock offset

If the offset is large (> 1 second), force an immediate sync:

```bash
# For chrony
sudo chronyc makestep

# For ntpd
sudo ntpdate -u pool.ntp.org
```

### No NTP sources available

Configure NTP servers in the appropriate config file:

```bash
# /etc/systemd/timesyncd.conf
[Time]
NTP=time.cloudflare.com pool.ntp.org

# /etc/chrony.conf
server time.cloudflare.com iburst
server pool.ntp.org iburst
```

## Docker Considerations

Docker containers inherit the host's system time. Ensure:

1. The **host** system has NTP configured (containers don't need their own)
2. Don't mount `/etc/localtime` read-write in containers
3. Use UTC timezone in containers to avoid DST issues

```yaml
# docker-compose.yaml
services:
  myservice:
    environment:
      - TZ=UTC
```

## Monitoring

For production, consider monitoring NTP status:

- Alert if clock offset exceeds 500ms
- Alert if NTP daemon is not running
- Include NTP status in health checks

## References

- [ADR-0026: Clock Skew Tolerance](../../ADRs/0026-clock-skew-tolerance.md)
- [chrony documentation](https://chrony.tuxfamily.org/documentation.html)
- [systemd-timesyncd](https://www.freedesktop.org/software/systemd/man/systemd-timesyncd.service.html)
