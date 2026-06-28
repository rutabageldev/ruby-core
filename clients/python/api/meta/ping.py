from http import HTTPStatus
from typing import Any, cast
from urllib.parse import quote

import httpx

from ...client import AuthenticatedClient, Client
from ...types import Response, UNSET
from ... import errors

from ...models.ping_response_200 import PingResponse200
from ...models.problem import Problem
from typing import cast



def _get_kwargs(
    
) -> dict[str, Any]:
    

    

    

    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/ping",
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> PingResponse200 | Problem:
    if response.status_code == 200:
        response_200 = PingResponse200.from_dict(response.json())



        return response_200

    if response.status_code == 401:
        response_401 = Problem.from_dict(response.json())



        return response_401

    response_default = Problem.from_dict(response.json())



    return response_default



def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[PingResponse200 | Problem]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    *,
    client: AuthenticatedClient | Client,

) -> Response[PingResponse200 | Problem]:
    """ Liveness of the authenticated API surface

     Returns a small payload confirming the API is reachable and the caller's bearer
    token was accepted. This is the placeholder endpoint that establishes the read
    platform; domain endpoints (calendar, directory, childcare) are added against it
    in later slices. Distinct from the unauthenticated `/health` probe, which lives
    outside the versioned, generated surface for Traefik and Uptime Kuma.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[PingResponse200 | Problem]
     """


    kwargs = _get_kwargs(
        
    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    *,
    client: AuthenticatedClient | Client,

) -> PingResponse200 | Problem | None:
    """ Liveness of the authenticated API surface

     Returns a small payload confirming the API is reachable and the caller's bearer
    token was accepted. This is the placeholder endpoint that establishes the read
    platform; domain endpoints (calendar, directory, childcare) are added against it
    in later slices. Distinct from the unauthenticated `/health` probe, which lives
    outside the versioned, generated surface for Traefik and Uptime Kuma.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        PingResponse200 | Problem
     """


    return sync_detailed(
        client=client,

    ).parsed

async def asyncio_detailed(
    *,
    client: AuthenticatedClient | Client,

) -> Response[PingResponse200 | Problem]:
    """ Liveness of the authenticated API surface

     Returns a small payload confirming the API is reachable and the caller's bearer
    token was accepted. This is the placeholder endpoint that establishes the read
    platform; domain endpoints (calendar, directory, childcare) are added against it
    in later slices. Distinct from the unauthenticated `/health` probe, which lives
    outside the versioned, generated surface for Traefik and Uptime Kuma.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[PingResponse200 | Problem]
     """


    kwargs = _get_kwargs(
        
    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    *,
    client: AuthenticatedClient | Client,

) -> PingResponse200 | Problem | None:
    """ Liveness of the authenticated API surface

     Returns a small payload confirming the API is reachable and the caller's bearer
    token was accepted. This is the placeholder endpoint that establishes the read
    platform; domain endpoints (calendar, directory, childcare) are added against it
    in later slices. Distinct from the unauthenticated `/health` probe, which lives
    outside the versioned, generated surface for Traefik and Uptime Kuma.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        PingResponse200 | Problem
     """


    return (await asyncio_detailed(
        client=client,

    )).parsed
