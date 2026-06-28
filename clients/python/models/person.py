from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar, BinaryIO, TextIO, TYPE_CHECKING, Generator

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

from ..types import UNSET, Unset






T = TypeVar("T", bound="Person")



@_attrs_define
class Person:
    """ A person or group in the household directory — feeds the "FOR" subject picker and provider linking. Local overlay,
    never written to Google.

        Example:
            {'id': '11111111-1111-4111-8111-111111111111', 'display_name': 'Katie', 'kind': 'person', 'email':
                'michael.katie.rubanka@gmail.com', 'family': 'Rubanka', 'active': True}

        Attributes:
            id (str): The person's stable id (UUID).
            display_name (str): Human-readable name.
            kind (str): Either `person` or `group` (a group like "Household" has no membership in MVP).
            active (bool): Whether the person is active.
            ha_person_entity_id (str | Unset): The Home Assistant person entity id this maps to, when present.
            email (str | Unset): Email used to reconcile calendar attendees to this person, when present.
            family (str | Unset): Family grouping used to colour-by-family in clients.
            color (str | Unset): Display colour, when set.
     """

    id: str
    display_name: str
    kind: str
    active: bool
    ha_person_entity_id: str | Unset = UNSET
    email: str | Unset = UNSET
    family: str | Unset = UNSET
    color: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        id = self.id

        display_name = self.display_name

        kind = self.kind

        active = self.active

        ha_person_entity_id = self.ha_person_entity_id

        email = self.email

        family = self.family

        color = self.color


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "id": id,
            "display_name": display_name,
            "kind": kind,
            "active": active,
        })
        if ha_person_entity_id is not UNSET:
            field_dict["ha_person_entity_id"] = ha_person_entity_id
        if email is not UNSET:
            field_dict["email"] = email
        if family is not UNSET:
            field_dict["family"] = family
        if color is not UNSET:
            field_dict["color"] = color

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        id = d.pop("id")

        display_name = d.pop("display_name")

        kind = d.pop("kind")

        active = d.pop("active")

        ha_person_entity_id = d.pop("ha_person_entity_id", UNSET)

        email = d.pop("email", UNSET)

        family = d.pop("family", UNSET)

        color = d.pop("color", UNSET)

        person = cls(
            id=id,
            display_name=display_name,
            kind=kind,
            active=active,
            ha_person_entity_id=ha_person_entity_id,
            email=email,
            family=family,
            color=color,
        )


        person.additional_properties = d
        return person

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
