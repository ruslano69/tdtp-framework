"""
Pandas integration for TDTP DataFrame

Optional module - requires pandas to be installed
"""

try:
    import pandas as pd
    HAS_PANDAS = True
except ImportError:
    HAS_PANDAS = False


def to_pandas(df):
    """
    Convert TDTP DataFrame to pandas DataFrame

    Args:
        df: TDTP DataFrame

    Returns:
        pandas.DataFrame

    Raises:
        ImportError: If pandas is not installed
    """
    if not HAS_PANDAS:
        raise ImportError(
            "pandas is not installed. "
            "Install it with: pip install pandas"
        )

    # Direct conversion - pandas handles list of lists efficiently
    return pd.DataFrame(df.data, columns=df.columns)


def from_pandas(pdf):
    """
    Convert pandas DataFrame to TDTP DataFrame

    Args:
        pdf: pandas.DataFrame

    Returns:
        TDTP DataFrame

    Raises:
        ImportError: If pandas is not installed
    """
    if not HAS_PANDAS:
        raise ImportError(
            "pandas is not installed. "
            "Install it with: pip install pandas"
        )

    from .core import DataFrame

    # Convert to list of lists
    data = pdf.values.tolist()

    # Create schema from pandas dtypes
    fields = []
    for col, dtype in pdf.dtypes.items():
        # Map pandas dtype to TDTP type
        if dtype == 'int64' or dtype == 'int32':
            tdtp_type = 'int'
        elif dtype == 'float64' or dtype == 'float32':
            tdtp_type = 'decimal'
        elif dtype == 'bool':
            tdtp_type = 'boolean'
        elif dtype == 'datetime64[ns]':
            tdtp_type = 'datetime'
        else:
            tdtp_type = 'string'

        fields.append({
            'name': str(col),
            'type': tdtp_type
        })

    schema = {'Fields': fields}

    return DataFrame(data, schema)
