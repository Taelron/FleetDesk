"""dbus.connection stub: the private bus RegisterServer.Start hands back."""


class Connection(object):
    def __init__(self, address):
        self.address = address

    def get_object(self, name, path, introspect=True):
        from dbus import _Obj
        return _Obj(path)
