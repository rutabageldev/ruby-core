from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar, BinaryIO, TextIO, TYPE_CHECKING, Generator

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset







T = TypeVar("T", bound="ProviderSuggestion")



@_attrs_define
class ProviderSuggestion:
    """ A non-archived provider ranked by recent per-occurrence usage (higher score = more recently/frequently used).

        Example:
            {'id': '22222222-2222-4222-8222-222222222222', 'display_name': 'Sue', 'score': 4.5}

        Attributes:
            id (str): The provider's id (UUID).
            display_name (str): Human-readable name.
            score (float): Recency-weighted per-occurrence usage score over the recency window.
     """

    id: str
    display_name: str
    score: float
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        id = self.id

        display_name = self.display_name

        score = self.score


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "id": id,
            "display_name": display_name,
            "score": score,
        })

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        id = d.pop("id")

        display_name = d.pop("display_name")

        score = d.pop("score")

        provider_suggestion = cls(
            id=id,
            display_name=display_name,
            score=score,
        )


        provider_suggestion.additional_properties = d
        return provider_suggestion

    @property
    def additional_keys(self) -> list[str]:
        return list(self.additional_properties.keys())

    def __getitem__(self, key: str) -> Any:
        return self.additional_properties[key]

    def __setitem__(self, key: str, value: Any) -> None:
        self.additional_properties[key] = value

    def __delitem__(self, key: str) -> None:
        del self.additional_properties[key]

    def __contains__(self, key: str) -> bool:
        return key in self.additional_properties
