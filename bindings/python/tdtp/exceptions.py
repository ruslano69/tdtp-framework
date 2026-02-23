"""TDTP library exceptions."""


class TDTPError(Exception):
    """Base exception for all TDTP errors."""


class TDTPParseError(TDTPError):
    """Raised when a .tdtp file cannot be parsed."""


class TDTPFilterError(TDTPError):
    """Raised when a TDTQL WHERE clause is invalid or filter fails."""


class TDTPProcessorError(TDTPError):
    """Raised when a processor (mask / normalize / validate / compress) fails."""


class TDTPWriteError(TDTPError):
    """Raised when writing a .tdtp file fails."""


class TDTPLibraryError(TDTPError):
    """Raised when the native libtdtp.so cannot be loaded."""
