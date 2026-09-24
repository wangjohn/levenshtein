"""Settings for the secrets-clean fixture.

The key is read from the environment, never written down, so the secrets check
has nothing to report.
"""

import os

DEBUG = False
api_key = os.environ["API_KEY"]
