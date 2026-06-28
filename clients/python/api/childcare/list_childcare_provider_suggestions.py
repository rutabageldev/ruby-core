from http import HTTPStatus
from typing import Any, cast
from urllib.parse import quote

import httpx

from ...client import AuthenticatedClient, Client
from ...types import Response, UNSET
from ... import errors

from ...models.problem import Problem
from ...models.provider_suggestion import ProviderSuggestion
from typing import cast



def _get_kwargs(
    
) -> dict[str, Any]:
    

    

    

    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/childcare/providers/suggestions",
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Problem | list[ProviderSuggestion]:
    if response.status_code == 200:
        response_200 = []
        _response_200 = response.json()
        for response_200_item_data in (_response_200):
            response_200_item = ProviderSuggestion.from_dict(response_200_item_data)



            response_200.append(response_200_item)

        return response_200

    if response.status_code == 401:
        response_401 = Problem.from_dict(response.json())



        return response_401

    response_default = Problem.from_dict(response.json())



    return response_default



def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[Problem | list[ProviderSuggestion]]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    *,
    client: AuthenticatedClient | Client,

) -> Response[Problem | list[ProviderSuggestion]]:
    """ List childcare providers ranked by recent usage

     Returns non-archived providers ranked by per-occurrence recent usage: each
    provider's associated event series' past occurrences are expanded over a recency
    window, counted per occurrence, and recency-weighted (a weekly slot counts as
    many; a one-off counts as one). Nothing is stored — the ranking is computed from
    associations plus expansion, so it can't drift.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[Problem | list[ProviderSuggestion]]
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

) -> Problem | list[ProviderSuggestion] | None:
    """ List childcare providers ranked by recent usage

     Returns non-archived providers ranked by per-occurrence recent usage: each
    provider's associated event series' past occurrences are expanded over a recency
    window, counted per occurrence, and recency-weighted (a weekly slot counts as
    many; a one-off counts as one). Nothing is stored — the ranking is computed from
    associations plus expansion, so it can't drift.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Problem | list[ProviderSuggestion]
     """


    return sync_detailed(
        client=client,

    ).parsed

async def asyncio_detailed(
    *,
    client: AuthenticatedClient | Client,

) -> Response[Problem | list[ProviderSuggestion]]:
    """ List childcare providers ranked by recent usage

     Returns non-archived providers ranked by per-occurrence recent usage: each
    provider's associated event series' past occurrences are expanded over a recency
    window, counted per occurrence, and recency-weighted (a weekly slot counts as
    many; a one-off counts as one). Nothing is stored — the ranking is computed from
    associations plus expansion, so it can't drift.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[Problem | list[ProviderSuggestion]]
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

) -> Problem | list[ProviderSuggestion] | None:
    """ List childcare providers ranked by recent usage

     Returns non-archived providers ranked by per-occurrence recent usage: each
    provider's associated event series' past occurrences are expanded over a recency
    window, counted per occurrence, and recency-weighted (a weekly slot counts as
    many; a one-off counts as one). Nothing is stored — the ranking is computed from
    associations plus expansion, so it can't drift.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Problem | list[ProviderSuggestion]
     """


    return (await asyncio_detailed(
        client=client,

    )).parsed
