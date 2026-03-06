import gauntlet_sdk as gauntlet


def test_gauntlet_sdk_namespace_exports_public_api() -> None:
    assert callable(gauntlet.connect)
    assert callable(gauntlet.disconnect)
    assert callable(gauntlet.tool)
