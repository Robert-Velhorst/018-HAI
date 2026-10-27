"""Scalar dtype conversion for Evidently's legacy column-type interface.

Pandas 3.1 removed the private helper imported by Evidently 0.7.23. Use
public Pandas/NumPy dtype interfaces, retaining legacy aliases and rejection
of overly specific datetime units. Do not modify Pandas module globals.
"""
import numpy as np
import pandas as pd
from pandas.api.extensions import ExtensionDtype
from pandas.api.types import pandas_dtype


def infer_dtype_from_object(value):
    if isinstance(value, type) and issubclass(value, np.generic):
        return value
    original_dtype_object = isinstance(value, (np.dtype, ExtensionDtype))
    if isinstance(value, str):
        if value in {"datetimetz", "datetime64tz"}:
            return pd.DatetimeTZDtype.type
        if value == "period":
            raise NotImplementedError("A concrete period dtype is required")
        value = {"datetime": "datetime64", "timedelta": "timedelta64"}.get(value, value)
    dtype = pandas_dtype(value)
    if original_dtype_object or isinstance(dtype, np.dtype):
        try:
            unit = np.datetime_data(dtype)[0]
        except (TypeError, ValueError):
            pass
        else:
            if unit not in {"generic", "ns"}:
                raise ValueError(f"{dtype.name!r} has an overly specific datetime unit")
    if original_dtype_object and hasattr(dtype, "numpy_dtype"):
        return dtype.numpy_dtype.type
    return dtype.type
