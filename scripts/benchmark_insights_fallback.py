import asyncio
import time

class MockXService:
    def __init__(self, latency: float = 0.05):
        self.latency = latency

    async def resolve_user_fallback(self, user_id: int) -> str:
        await asyncio.sleep(self.latency)
        return f"user_{user_id}"

async def sequential_fallback(x_service: MockXService, missing_ids: list[int]) -> dict[int, str]:
    user_map = {}
    for uid in missing_ids:
        user_map[uid] = await x_service.resolve_user_fallback(uid)
    return user_map

async def concurrent_fallback(x_service: MockXService, missing_ids: list[int]) -> dict[int, str]:
    handles = await asyncio.gather(*(x_service.resolve_user_fallback(uid) for uid in missing_ids))
    return dict(zip(missing_ids, handles))

async def main():
    service = MockXService(latency=0.05)
    missing_ids = list(range(1000, 1020)) # 20 user IDs

    start = time.perf_counter()
    res_seq = await sequential_fallback(service, missing_ids)
    seq_time = time.perf_counter() - start

    start = time.perf_counter()
    res_conc = await concurrent_fallback(service, missing_ids)
    conc_time = time.perf_counter() - start

    assert res_seq == res_conc
    print(f"Sequential time: {seq_time:.4f}s")
    print(f"Concurrent time: {conc_time:.4f}s")
    print(f"Speedup: {seq_time / conc_time:.2f}x")

if __name__ == "__main__":
    asyncio.run(main())
