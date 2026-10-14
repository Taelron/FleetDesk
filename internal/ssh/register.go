package ssh

import (
	"fmt"
	"strings"
)

// RegisterTag labels the activation key's line on the registration helper's
// stdin: "FLEETDESK_AK=<key>".
const RegisterTag = "FLEETDESK_AK"

// RegisterExitRHELTooOld is the exit status of the registration consumer on a
// target that is older than RHEL 8, or has no python3 with a D-Bus binding:
// RHSM's D-Bus API does not exist there, and registration never falls back to
// the subscription-manager CLI, which only accepts an activation key as an
// argument.
const RegisterExitRHELTooOld = 98

// RegisterRHELTooOldMessage is what the user sees for RegisterExitRHELTooOld.
const RegisterRHELTooOldMessage = "registration requires RHEL 8 or later (RHSM D-Bus API)"

// rhsmRegisterHelper registers the host through RHSM's D-Bus API,
// RegisterWithActivationKeys. It reads the activation key from stdin — the
// line "FLEETDESK_AK=<key>" — so the key is in no process's argv, and it
// prints only the consumer's name and uuid and, on failure, the D-Bus error
// name and message, which never contain the key.
//
// It contains no single quote and no percent sign: it is placed inside a
// single-quoted shell word, and nothing formats it. Its exit statuses: 98
// when python3 has no D-Bus binding (RHEL 7 and older), 2 when no key
// arrived, 1 when RHSM refused the registration.
const rhsmRegisterHelper = `import sys
try:
    import dbus
    import dbus.connection
except ImportError:
    print("the python3 D-Bus binding is not installed on this host")
    sys.exit(98)
key = None
for line in sys.stdin:
    if line.startswith("FLEETDESK_AK="):
        key = line[len("FLEETDESK_AK="):].rstrip("\n")
        break
if not key:
    print("no activation key reached the registration helper")
    sys.exit(2)
org = sys.argv[1]
status = 1
server = None
try:
    bus = dbus.SystemBus()
    server = dbus.Interface(bus.get_object("com.redhat.RHSM1", "/com/redhat/RHSM1/RegisterServer"), "com.redhat.RHSM1.RegisterServer")
    address = server.Start("")
    conn = dbus.connection.Connection(address)
    register = dbus.Interface(conn.get_object("com.redhat.RHSM1", "/com/redhat/RHSM1/Register"), "com.redhat.RHSM1.Register")
    result = register.RegisterWithActivationKeys(org, [key], {"force": True}, {}, "", timeout=600)
    name = uuid = ""
    try:
        import json
        consumer = json.loads(str(result))
        name = consumer.get("name", "")
        uuid = consumer.get("uuid", "")
    except Exception:
        pass
    print("Registered", name, uuid)
    status = 0
except dbus.DBusException as e:
    print("registration failed:", e.get_dbus_name(), e.get_dbus_message())
except Exception as e:
    print("registration failed:", type(e).__name__)
finally:
    if server is not None:
        try:
            server.Stop("")
        except Exception:
            pass
sys.exit(status)
`

// RegisterConsumer is the command the registration runs as root, with the
// activation key on its stdin: it refuses a target older than RHEL 8 (exit
// RegisterExitRHELTooOld, never the CLI), then runs the D-Bus helper. The org
// ID is not a secret and stays on argv.
func RegisterConsumer(orgID string) string {
	return registerPreflight + fmt.Sprintf("python3 -I -c '%s' '%s'", rhsmRegisterHelper, shQuote(orgID))
}

// registerPreflight refuses a target older than RHEL 8, or without python3,
// with exit 98. The helper is then run with -I (isolated mode): it runs as
// root with the SSH user's home as its working directory, and without -I a
// dbus.py planted there, or in PYTHONPATH, would be imported by root.
const registerPreflight = `v=$(. /etc/os-release 2>/dev/null; echo "${VERSION_ID%%.*}"); ` +
	`[ -n "$v" ] || v=$(sed -n 's/^.*release \([0-9][0-9]*\).*$/\1/p' /etc/redhat-release 2>/dev/null); ` +
	`[ "${v:-0}" -ge 8 ] 2>/dev/null || exit 98; ` +
	`command -v python3 >/dev/null 2>&1 || exit 98; `

// RegisterPrefix is what runs before the registration: nothing for the Red
// Hat CDN, and for Satellite the clean and the katello CA consumer install.
// Its "sudo " occurrences are rewritten into the password relay.
func RegisterPrefix(satelliteURL string) string {
	if satelliteURL == "" {
		return ""
	}
	return fmt.Sprintf("sudo subscription-manager clean && sudo dnf install -y 'http://%s/pub/katello-ca-consumer-latest.noarch.rpm' --disablerepo='*'",
		shQuote(satelliteURL))
}

// shQuote escapes single quotes for interpolation inside a single-quoted
// shell word.
func shQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}
