from __future__ import annotations


class RepoValidationError(ValueError):
    """Validation error for project repository resolution."""


class RepoBaseMissingError(RepoValidationError):
    pass


class RepoMissingError(RepoValidationError):
    pass


class RepoNotGitError(RepoValidationError):
    pass


class RepoSecurityError(RepoValidationError):
    pass
