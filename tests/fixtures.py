import pytest


@pytest.fixture
def set_namespace(request):
    ns = getattr(request.module, "NAMESPACE", "aaa")
    return ns
