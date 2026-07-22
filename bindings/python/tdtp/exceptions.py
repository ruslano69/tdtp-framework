"""TDTP library exceptions."""


class TDTPError(Exception):
    """Base exception for all TDTP errors.

    Carries the machine-readable ``code`` from the native error taxonomy
    (e.g. ``"PARSE_ERROR"``, ``"FILTER_ERROR"``) when available, so agents can
    branch on a stable identifier rather than the human-readable message.
    """

    def __init__(self, message: str = "", code: str = "") -> None:
        super().__init__(message)
        self.code = code


class TDTPParseError(TDTPError):
    """Raised when a .tdtp file cannot be parsed."""


class TDTPEncryptedPacketError(TDTPParseError):
    """Raised when reading a TDTP v1.5 encrypted packet directly.

    libtdtp is a pure parse/compress library with no xZMercury client, so it
    cannot decrypt QueryContext/Schema/Data ciphertext. Use the tdtpcli
    binary instead: ``tdtpcli --import --mercury-url <url>`` (requires a
    reachable xZMercury server for burn-on-read key retrieval).
    """


class TDTPFilterError(TDTPError):
    """Raised when a TDTQL WHERE clause is invalid or filter fails."""


class TDTPProcessorError(TDTPError):
    """Raised when a processor (mask / normalize / validate / compress) fails."""


class TDTPWriteError(TDTPError):
    """Raised when writing a .tdtp file fails."""


class TDTPLibraryError(TDTPError):
    """Raised when the native libtdtp.so cannot be loaded."""
