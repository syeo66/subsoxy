# Subsonic API Proxy Server

A high-performance Go-based proxy server that enhances your Subsonic music server with intelligent features like personalized song recommendations, play tracking, and multi-user support.

## 🎵 What It Does

**Subsoxy** sits between your music client and Subsonic server to add powerful features:

- **🎯 Smart Shuffle**: Personalized song recommendations with robust 2-week replay prevention for both played and skipped songs
- **📊 Play Tracking**: Automatic monitoring of what you play, skip, and enjoy
- **👥 Multi-User**: Complete isolation - each user gets their own personalized experience  
- **🔄 Auto-Sync**: Keeps your music library updated automatically
- **🎨 Cover Art**: Full cover art support in shuffled song responses
- **🛡️ Secure**: Enterprise-grade security with encrypted credential storage
- **⚡ Fast**: Connection pooling and optimized algorithms for smooth performance

## 🚀 Quick Start

### 1. Install
```bash
go build -o subsoxy
```

### 2. Run
```bash
# Basic setup - connects to Subsonic server at localhost:4533
./subsoxy

# Custom Subsonic server
./subsoxy -upstream http://my-subsonic-server:4533 -port 8080
```

### 3. Use
Point your music client to `http://localhost:8080` instead of your Subsonic server. All your existing apps work without changes!

```bash
# Your music client now connects to:
http://localhost:8080/rest/...

# Instead of directly to:  
http://my-subsonic-server:4533/rest/...
```

That's it! Subsoxy will automatically:
- ✅ Capture your credentials securely on first use
- ✅ Sync your music library immediately  
- ✅ Start learning your preferences
- ✅ Provide intelligent shuffle recommendations

## ⭐ Key Features

### Intelligent Music Recommendations
Your `/rest/getRandomSongs` requests now return personalized recommendations instead of random songs:
- **Learns Your Taste**: Tracks what you play vs skip with enhanced, preload-resistant skip detection
- **2-Week Replay Prevention**: Songs are strictly excluded for 14 days after being played OR skipped with consistent timing and robust filtering
- **Smart Transitions**: Considers song flow and your listening patterns
- **Individual Learning**: Each user gets their own personalized experience
- **Cover Art Included**: Full cover art support in both JSON and XML responses

#### Enhanced Skip Detection ✅ **IMPROVED**
The system now implements robust, preload-resistant skip detection that handles modern music clients:
- **Preload Support**: Multiple track preloading doesn't trigger false skip detection
- **Real Skips**: Only counted when songs are never played or when later songs get played first
- **Timeout Protection**: Songs pending >5 minutes without scrobble are marked as skipped
- **Client Agnostic**: Works with aggressive preloading strategies used by modern clients
- **Precise Analytics**: Your skip counts reflect actual listening behavior, not technical preloading

### Multi-User Support ✅ **NEW**
- **Complete Isolation**: Each user has their own music library and preferences
- **Privacy First**: No data bleeding between users
- **Scales**: Supports unlimited users with optimal performance
- **Works with all clients**: Symfonium, DSub, and other modern Subsonic clients

### Automatic Music Library Sync
- **Immediate Sync**: New users get instant access - no waiting for hourly syncs
- **Smart Updates**: Automatically removes deleted songs while preserving your play history
- **Background Processing**: Never blocks your music streaming
- **Reliable**: Uses proper Subsonic API discovery methods

### Enterprise Security
- **Encrypted Storage**: AES-256-GCM encryption for all credentials
- **Modern Auth**: Supports both password and token-based authentication
- **Rate Limiting**: Protection against abuse and DoS attacks
- **Security Headers**: Comprehensive protection against web vulnerabilities

## 🎛️ Configuration

### Quick Examples
```bash
# Basic usage
./subsoxy

# Custom port and server
./subsoxy -port 9090 -upstream http://music.example.com:4533

# Debug mode
./subsoxy -log-level debug

# High-performance setup
./subsoxy -db-max-open-conns 50 -rate-limit-rps 200

# CORS for web apps
./subsoxy -cors-allow-origins "https://myapp.com,http://localhost:3000"
```

### Environment Variables
```bash
# Use environment variables instead of flags
export PORT=8080
export UPSTREAM_URL=http://my-subsonic-server:4533
export LOG_LEVEL=info
./subsoxy
```

## 📊 How It Works

1. **Transparent Proxy**: All requests flow through to your Subsonic server
2. **Smart Hooks**: Specific endpoints get enhanced with intelligent features
3. **Learning Engine**: Builds personalized models from your listening habits
4. **Isolated Data**: Each user gets their own private learning model

### Enhanced Endpoints

| Endpoint | Enhancement |
|----------|-------------|
| `/rest/getRandomSongs` | Intelligent shuffle with 2-week replay prevention and cover art |
| `/rest/stream` | Tracks song starts for learning |
| `/rest/scrobble` | Records plays/skips for personalization |
| All others | Transparent proxy with full compatibility |

## 🔧 Advanced Configuration

For detailed configuration options, see [Configuration Guide](docs/configuration.md).

Common settings:
- **Port**: `-port 8080` (default)
- **Database**: `-db-path ./music.db` (auto-created)
- **Rate Limiting**: `-rate-limit-rps 100` (requests per second)
- **Connection Pool**: `-db-max-open-conns 25` (database connections)
- **CORS**: `-cors-allow-origins "*"` (for web clients)

## 🏗️ Architecture

Subsoxy uses a modular architecture designed for reliability and performance:

- **Multi-tenant database** with complete user isolation
- **Connection pooling** for optimal database performance  
- **Memory-efficient algorithms** that scale to large music libraries
- **Comprehensive error handling** with structured logging
- **Thread-safe operations** for concurrent users

For technical details, see [Architecture Guide](docs/architecture.md).

## 🛡️ Security

Security is built-in, not bolted-on:

- **AES-256-GCM encryption** for credential storage
- **Rate limiting** to prevent abuse
- **Input validation** and sanitization
- **Security headers** for web vulnerability protection
- **Multi-mode authentication** (password + token support)

For security details, see [Security Guide](docs/security.md).

## 🎯 Multi-User Features

Perfect for families, shared servers, or multiple music libraries:

- **Complete Data Isolation**: Each user's data is completely separate
- **Individual Preferences**: Personal recommendations for every user
- **Modern Client Support**: Works with Symfonium, DSub, and other apps
- **Scalable**: Handles unlimited users efficiently

For multi-tenancy details, see [Multi-Tenancy Guide](docs/multi-tenancy.md).

## 📈 Performance

Optimized for real-world usage:

- **Memory Efficient**: Handles 100,000+ song libraries
- **Fast Queries**: Optimized database operations with connection pooling
- **Smart Algorithms**: Automatically adapts to library size
- **Concurrent Access**: Thread-safe for multiple simultaneous users

For performance details, see [Weighted Shuffle Guide](docs/weighted-shuffle.md).

## 🧪 Testing ✅ **ENHANCED**

```bash
# Run all tests with comprehensive coverage
go test ./...

# Run with race detection (recommended)
go test ./... -race

# Run enhanced shuffle algorithm tests
go test ./shuffle -v -run="TestCalculateSongWeight"

# Test boundary conditions and edge cases
go test ./shuffle -v -run="TestCalculateSongWeightBoundaryConditions"

# Test with real Subsonic server
./subsoxy -upstream https://your-server.com &
curl "http://localhost:8080/rest/ping?u=user&p=pass&f=json"
```

## 📖 Documentation

- [**Configuration Guide**](docs/configuration.md) - Complete configuration reference
- [**Architecture Guide**](docs/architecture.md) - Technical architecture details  
- [**Security Guide**](docs/security.md) - Security features and best practices
- [**Multi-Tenancy Guide**](docs/multi-tenancy.md) - Multi-user setup and features
- [**Database Guide**](docs/database.md) - Database schema and features
- [**Weighted Shuffle Guide**](docs/weighted-shuffle.md) - How intelligent recommendations work
- [**Development Guide**](docs/development.md) - Contributing and development setup

## 🆘 Getting Help

- Check the [Configuration Guide](docs/configuration.md) for setup issues
- Review [Security Guide](docs/security.md) for authentication problems  
- See [Development Guide](docs/development.md) for contributing

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

---

**Ready to enhance your music experience?** 
```bash
go build -o subsoxy && ./subsoxy
```

Then point your music client to `http://localhost:8080` and enjoy intelligent, personalized music recommendations! 🎵