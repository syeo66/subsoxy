# Troubleshooting Guide

This guide covers common issues and their solutions when running the Subsonic Proxy Server.

## Configuration Issues

### Invalid Port Number

**Error**: `[config:INVALID_PORT] port must be a number`

**Causes:**
- Non-numeric port value in command line or environment variable
- Port number outside valid range (1-65535)

**Solutions:**
```bash
# Fix non-numeric port
./subsoxy -port 8080  # Instead of -port abc

# Fix out-of-range port  
./subsoxy -port 8080  # Instead of -port 70000

# Check environment variables
echo $PORT  # Should be empty or valid number
unset PORT  # Clear invalid environment variable
```

### Invalid Upstream URL

**Error**: `[config:INVALID_UPSTREAM_URL] invalid upstream URL format`

**Causes:**
- Malformed URL syntax
- Missing protocol (http/https)
- Missing hostname

**Solutions:**
```bash
# Fix missing protocol
./subsoxy -upstream http://localhost:4533  # Instead of localhost:4533

# Fix invalid characters
./subsoxy -upstream http://my-server:4533  # Instead of http://my server:4533

# Verify URL is reachable
curl http://localhost:4533/rest/ping
```

### Database Path Issues

**Error**: `[config:INVALID_DATABASE_PATH] cannot create database directory`

**Causes:**
- Permission denied for directory creation
- Invalid path characters
- Filesystem full

**Solutions:**
```bash
# Fix permissions
chmod 755 /path/to/database/  # Ensure directory is writable
sudo chown $USER /path/to/database/  # Fix ownership

# Use relative path
./subsoxy -db-path ./data/music.db  # Create in current directory

# Check disk space
df -h  # Ensure sufficient space available
```

## Database Issues

### Connection Failed

**Error**: `[database:CONNECTION_FAILED] failed to open database`

**Causes:**
- File permissions
- Database file locked by another process
- Corrupted database file
- Insufficient disk space

**Solutions:**
```bash
# Check file permissions
ls -la subsoxy.db
chmod 644 subsoxy.db  # Fix permissions

# Check for locks
lsof subsoxy.db  # See if file is locked
fuser subsoxy.db  # Alternative lock check

# Backup and recreate database
mv subsoxy.db subsoxy.db.backup
./subsoxy  # Will create new database

# Check disk space
df -h .  # Ensure space available
```

### Query Failed

**Error**: `[database:QUERY_FAILED] failed to execute query`

**Causes:**
- Database corruption
- Schema version mismatch
- SQLite version incompatibility

**Solutions:**
```bash
# Check database integrity
sqlite3 subsoxy.db "PRAGMA integrity_check;"

# Check schema
sqlite3 subsoxy.db ".schema"

# Recreate database if corrupted
rm subsoxy.db
./subsoxy  # Will recreate with current schema
```

## Network Issues

### Upstream Server Unreachable

**Error**: `[network:UPSTREAM_ERROR] failed to fetch songs from Subsonic API`

**Causes:**
- Upstream server down
- Network connectivity issues
- Firewall blocking connection
- DNS resolution problems

**Solutions:**
```bash
# Test connectivity
ping my-subsonic-server
curl http://my-subsonic-server:4533/rest/ping

# Check DNS resolution
nslookup my-subsonic-server
dig my-subsonic-server

# Test from proxy server
telnet my-subsonic-server 4533

# Check firewall rules
sudo iptables -L  # Linux
sudo ufw status   # Ubuntu
```

### Timeout Issues

**Error**: `[network:TIMEOUT] network timeout`

**Causes:**
- Slow network connection
- Upstream server overloaded
- Network congestion

**Solutions:**
```bash
# Test network latency
ping -c 5 my-subsonic-server

# Check server load
curl -w "%{time_total}\n" http://my-subsonic-server:4533/rest/ping

# Monitor network usage
iftop  # or nethogs, nload
```

## Security Issues

### Password Logging Vulnerability (Fixed)

**Previous Issue**: Passwords were exposed in server logs during song synchronization

**Security Risk**: 
- Passwords visible in log files
- Potential credential exposure in debug output
- Risk of credential leakage through log aggregation systems

**Fix Applied**:
- ✅ **RESOLVED**: Updated `server/server.go` to use secure URL parameter encoding
- Passwords are now properly encoded using `url.Values{}` instead of direct string formatting
- No more credential exposure in logs, debug output, or error messages

**Verification**:
```bash
# Check that passwords are not in logs
grep -i password /var/log/subsoxy.log  # Should return no results

# Verify secure URL construction
# Look for "URL_PARSE_FAILED" errors instead of exposed credentials
```

**Impact**: 
- All password logging vulnerabilities eliminated
- Maintains full functionality while securing credential handling
- Follows the same secure pattern used in credential validation

## Authentication Issues

### Invalid Credentials

**Error**: `[credentials:INVALID_CREDENTIALS] invalid credentials`

**Causes:**
- Wrong username/password
- Upstream server authentication failure
- Account locked or disabled

**Solutions:**
```bash
# Test credentials directly
curl "http://upstream:4533/rest/ping?u=user&p=pass&c=test&f=json"

# Check account status on upstream server
# Login to Subsonic web interface

# Verify URL encoding
# Ensure special characters in password are URL-encoded
```

### No Valid Credentials

**Error**: `[credentials:NO_VALID_CREDENTIALS] no valid credentials available`

**Causes:**
- No clients have connected yet
- All stored credentials became invalid
- Credential validation failed

**Solutions:**
```bash
# Connect with a Subsonic client first
curl "http://localhost:8080/rest/ping?u=user&p=pass&c=test&f=json"

# Check proxy logs for credential validation
./subsoxy -log-level debug

# Verify upstream server is accessible
curl http://upstream:4533/rest/ping
```

## Performance Issues

### High Memory Usage

**Symptoms:**
- Process memory grows over time
- System becomes slow
- Out of memory errors

**Solutions:**
```bash
# Monitor memory usage
top -p $(pgrep subsoxy)
ps aux | grep subsoxy

# Check database size
du -h subsoxy.db

# Clean up old data (if needed)
sqlite3 subsoxy.db "DELETE FROM play_events WHERE timestamp < datetime('now', '-1 year');"
sqlite3 subsoxy.db "VACUUM;"
```

### High CPU Usage

**Symptoms:**
- High CPU usage in top/htop
- Slow response times
- System lag

**Solutions:**
```bash
# Check for excessive requests
tail -f /var/log/syslog | grep subsoxy

# Monitor request patterns
./subsoxy -log-level debug

# Check database query performance
sqlite3 subsoxy.db "EXPLAIN QUERY PLAN SELECT * FROM songs;"

# Analyze slow queries
sqlite3 subsoxy.db "PRAGMA analysis;"
```

## Client Connection Issues

### Proxy Not Responding

**Symptoms:**
- Connection refused errors
- Timeouts from clients
- No response from proxy

**Solutions:**
```bash
# Check if proxy is running
ps aux | grep subsoxy
pgrep subsoxy

# Check port binding
netstat -tlnp | grep 8080
ss -tlnp | grep 8080

# Test proxy directly
curl http://localhost:8080/rest/ping

# Check logs for errors
./subsoxy -log-level debug
```

### Requests Not Forwarded

**Symptoms:**
- Proxy responds but upstream doesn't receive requests
- Hooks work but forwarding fails
- Partial responses

**Solutions:**
```bash
# Test upstream connectivity
curl http://upstream:4533/rest/ping

# Check proxy configuration
./subsoxy -log-level debug

# Verify upstream URL
echo $UPSTREAM_URL
```

## Logging and Debugging

### Enable Debug Logging

```bash
# Command line
./subsoxy -log-level debug

# Environment variable
LOG_LEVEL=debug ./subsoxy

# Check specific modules
# Look for error patterns in logs
```

### Log Analysis

```bash
# Monitor logs in real-time
./subsoxy -log-level debug 2>&1 | tee subsoxy.log

# Search for errors
grep "ERROR" subsoxy.log
grep "\[database:" subsoxy.log
grep "\[network:" subsoxy.log

# Check error context
grep -A 5 -B 5 "CONNECTION_FAILED" subsoxy.log
```

### Common Log Patterns

```bash
# Database issues
"[database:CONNECTION_FAILED]"
"[database:QUERY_FAILED]"

# Network issues  
"[network:UPSTREAM_ERROR]"
"[network:TIMEOUT]"

# Configuration issues
"[config:INVALID_PORT]"
"[config:INVALID_UPSTREAM_URL]"

# Authentication issues
"[credentials:VALIDATION_FAILED]"
"[credentials:INVALID_CREDENTIALS]"
```

## Getting Help

### Collecting Debug Information

When reporting issues, include:

1. **Error messages** with full context
2. **Configuration** (sanitized of passwords)
3. **Log output** with debug level enabled
4. **System information**:
   ```bash
   uname -a                    # System info
   go version                  # Go version
   sqlite3 --version          # SQLite version
   ./subsoxy --version        # If available
   ```

5. **Network connectivity**:
   ```bash
   curl -v http://upstream:4533/rest/ping
   traceroute upstream-server
   ```

### Environment Information

```bash
# Export sanitized configuration
env | grep -E "(PORT|UPSTREAM_URL|LOG_LEVEL|DB_PATH)" | sed 's/=.*password.*/=***PASSWORD***/'

# Check file permissions
ls -la subsoxy.db
ls -la .

# Check system resources
free -h
df -h
```

This troubleshooting guide covers the most common issues. For additional help, enable debug logging and examine the error context provided by the structured error handling system.