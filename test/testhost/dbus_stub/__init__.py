"""Stub of the dbus-python surface FleetDesk's registration helper uses.

TAE-116 (L2): the test host has no RHSM, so this module stands in for
dbus-python and records what the registration helper does. It is installed as
the system-wide `dbus` package in the container image only.

It implements: SystemBus().get_object(), Interface(obj, name) with
RegisterServer.Start/Stop and Register.RegisterWithActivationKeys, and
connection.Connection(address).get_object(). RegisterWithActivationKeys holds
for three seconds -- so the process doing the registration is resident long
enough to be observed in `ps` -- appends `org=<org> key_len=<n>` to
/run/testhost/register.log (the key's length, never the key), and returns a
consumer as JSON.
"""
import os
import time

REGISTER_LOG = "/run/testhost/register.log"
HOLD_SECONDS = 3


class DBusException(Exception):
    def get_dbus_name(self):
        return "com.redhat.RHSM1.Error"

    def get_dbus_message(self):
        return str(self)


class _Obj(object):
    def __init__(self, path):
        self.path = path


class SystemBus(object):
    def get_object(self, name, path, introspect=True):
        return _Obj(path)


class Interface(object):
    def __init__(self, obj, iface):
        self.obj = obj
        self.iface = iface

    def Start(self, locale):
        return "unix:path=/run/testhost/stub-rhsm-private-bus"

    def Stop(self, locale):
        return True

    def RegisterWithActivationKeys(self, org, keys, options, connection_options, locale, timeout=0):
        time.sleep(HOLD_SECONDS)
        os.makedirs(os.path.dirname(REGISTER_LOG), exist_ok=True)
        with open(REGISTER_LOG, "a") as f:
            f.write("org=%s key_len=%d\n" % (org, len(keys[0])))
        return '{"name": "testhost", "uuid": "stub"}'


from dbus import connection  # noqa: E402  (after the classes it imports)
