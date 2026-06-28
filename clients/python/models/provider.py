from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar, BinaryIO, TextIO, TYPE_CHECKING, Generator

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

from ..types import UNSET, Unset






T = TypeVar("T", bound="Provider")



@_attrs_define
class Provider:
    """ A childcare provider — may be a household person (e.g. a grandparent) or standalone (e.g. a daycare). Local overlay.

        Example:
            {'id': '22222222-2222-4222-8222-222222222222', 'display_name': 'Sue', 'relationship': 'grandparent', 'archived':
                False}

        Attributes:
            id (str): The provider's stable id (UUID).
            display_name (str): Human-readable name.
            archived (bool): Whether the provider has been archived (soft-deleted; frequency history is preserved).
            person_id (str | Unset): The linked directory person id, when the provider is also a household person.
            relationship (str | Unset): Free-text relationship to the household, when set.
     """

    id: str
    display_name: str
    archived: bool
    person_id: str | Unset = UNSET
    relationship: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        id = self.id

        display_name = self.display_name

        archived = self.archived

        person_id = self.person_id

        relationship = self.relationship


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "id": id,
            "display_name": display_name,
            "archived": archived,
        })
        if person_id is not UNSET:
            field_dict["person_id"] = person_id
        if relationship is not UNSET:
            field_dict["relationship"] = relationship

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        id = d.pop("id")

        display_name = d.pop("display_name")

        archived = d.pop("archived")

        person_id = d.pop("person_id", UNSET)

        relationship = d.pop("relationship", UNSET)

        provider = cls(
            id=id,
            display_name=display_name,
            archived=archived,
            person_id=person_id,
            relationship=relationship,
        )


        provider.additional_properties = d
        return provider

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
