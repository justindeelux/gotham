import{I as go,ct as Co,bI as r,K as bo,a1 as u,am as f,aK as z,J as I,d as uo,L as q,a5 as j,o as m,c as S,O as p,a as vo,W as k,m as fo,aw as po,V as L,P as ko,ad as mo,a3 as xo,y as K,q as yo,as as Po,S as i,b8 as zo,aJ as J,aD as Io,ah as So,av as Bo}from"./index-C1fVPH_1.js";function $o(o){const{textColor2:h,primaryColorHover:b,primaryColorPressed:v,primaryColor:l,infoColor:t,successColor:n,warningColor:a,errorColor:s,baseColor:g,borderColor:B,opacityDisabled:$,tagColor:H,closeIconColor:x,closeIconColorHover:y,closeIconColorPressed:e,borderRadiusSmall:c,fontSizeMini:C,fontSizeTiny:d,fontSizeSmall:R,fontSizeMedium:_,heightMini:M,heightTiny:T,heightSmall:E,heightMedium:W,closeColorHover:w,closeColorPressed:F,buttonColor2Hover:O,buttonColor2Pressed:V,fontWeightStrong:D}=o;return{...Co,closeBorderRadius:c,heightTiny:M,heightSmall:T,heightMedium:E,heightLarge:W,borderRadius:c,opacityDisabled:$,fontSizeTiny:C,fontSizeSmall:d,fontSizeMedium:R,fontSizeLarge:_,fontWeightStrong:D,textColorCheckable:h,textColorHoverCheckable:h,textColorPressedCheckable:h,textColorChecked:g,colorCheckable:"#0000",colorHoverCheckable:O,colorPressedCheckable:V,colorChecked:l,colorCheckedHover:b,colorCheckedPressed:v,border:`1px solid ${B}`,textColor:h,color:H,colorBordered:"rgb(250, 250, 252)",closeIconColor:x,closeIconColorHover:y,closeIconColorPressed:e,closeColorHover:w,closeColorPressed:F,borderPrimary:`1px solid ${r(l,{alpha:.3})}`,textColorPrimary:l,colorPrimary:r(l,{alpha:.12}),colorBorderedPrimary:r(l,{alpha:.1}),closeIconColorPrimary:l,closeIconColorHoverPrimary:l,closeIconColorPressedPrimary:l,closeColorHoverPrimary:r(l,{alpha:.12}),closeColorPressedPrimary:r(l,{alpha:.18}),borderInfo:`1px solid ${r(t,{alpha:.3})}`,textColorInfo:t,colorInfo:r(t,{alpha:.12}),colorBorderedInfo:r(t,{alpha:.1}),closeIconColorInfo:t,closeIconColorHoverInfo:t,closeIconColorPressedInfo:t,closeColorHoverInfo:r(t,{alpha:.12}),closeColorPressedInfo:r(t,{alpha:.18}),borderSuccess:`1px solid ${r(n,{alpha:.3})}`,textColorSuccess:n,colorSuccess:r(n,{alpha:.12}),colorBorderedSuccess:r(n,{alpha:.1}),closeIconColorSuccess:n,closeIconColorHoverSuccess:n,closeIconColorPressedSuccess:n,closeColorHoverSuccess:r(n,{alpha:.12}),closeColorPressedSuccess:r(n,{alpha:.18}),borderWarning:`1px solid ${r(a,{alpha:.35})}`,textColorWarning:a,colorWarning:r(a,{alpha:.15}),colorBorderedWarning:r(a,{alpha:.12}),closeIconColorWarning:a,closeIconColorHoverWarning:a,closeIconColorPressedWarning:a,closeColorHoverWarning:r(a,{alpha:.12}),closeColorPressedWarning:r(a,{alpha:.18}),borderError:`1px solid ${r(s,{alpha:.23})}`,textColorError:s,colorError:r(s,{alpha:.1}),colorBorderedError:r(s,{alpha:.08}),closeIconColorError:s,closeIconColorHoverError:s,closeIconColorPressedError:s,closeColorHoverError:r(s,{alpha:.12}),closeColorPressedError:r(s,{alpha:.18})}}const Ho={common:go,self:$o};var Ro={color:Object,type:{type:String,default:"default"},round:Boolean,size:String,closable:Boolean,disabled:{type:Boolean,default:void 0}},_o=bo("tag",`
 --n-close-margin: var(--n-close-margin-top) var(--n-close-margin-right) var(--n-close-margin-bottom) var(--n-close-margin-left);
 white-space: nowrap;
 position: relative;
 box-sizing: border-box;
 cursor: default;
 display: inline-flex;
 align-items: center;
 flex-wrap: nowrap;
 padding: var(--n-padding);
 border-radius: var(--n-border-radius);
 color: var(--n-text-color);
 background-color: var(--n-color);
 transition: 
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 line-height: 1;
 height: var(--n-height);
 font-size: var(--n-font-size);
`,[u("strong",`
 font-weight: var(--n-font-weight-strong);
 `),f("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),f("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),f("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),f("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),u("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[f("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),f("avatar",`
 margin: 0 6px 0 calc((var(--n-height) - 8px) / -2);
 `),u("closable",`
 padding: 0 calc(var(--n-height) / 4) 0 calc(var(--n-height) / 3);
 `)]),u("icon, avatar",[u("round",`
 padding: 0 calc(var(--n-height) / 3) 0 calc(var(--n-height) / 2);
 `)]),u("disabled",`
 cursor: not-allowed !important;
 opacity: var(--n-opacity-disabled);
 `),u("checkable",`
 cursor: pointer;
 box-shadow: none;
 color: var(--n-text-color-checkable);
 background-color: var(--n-color-checkable);
 `,[z("disabled",[I("&:hover","background-color: var(--n-color-hover-checkable);",[z("checked","color: var(--n-text-color-hover-checkable);")]),I("&:active","background-color: var(--n-color-pressed-checkable);",[z("checked","color: var(--n-text-color-pressed-checkable);")])]),u("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[z("disabled",[I("&:hover","background-color: var(--n-color-checked-hover);"),I("&:active","background-color: var(--n-color-checked-pressed);")])])])]);const Mo=["onClick","onMouseenter","onMouseleave"],To={...q.props,...Ro,bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function},Eo=Io("n-tag");var wo=uo({name:"Tag",props:To,slots:Object,setup(o){const h=yo(null),{mergedBorderedRef:b,mergedClsPrefixRef:v,inlineThemeDisabled:l,mergedRtlRef:t,mergedComponentPropsRef:n}=ko(o),a=K(()=>o.size||n?.value?.Tag?.size||"medium"),s=q("Tag","-tag",_o,Ho,o,v);So(Eo,{roundRef:Bo(o,"round")});function g(){if(!o.disabled&&o.checkable){const{checked:e,onCheckedChange:c,onUpdateChecked:C,"onUpdate:checked":d}=o;C&&C(!e),d&&d(!e),c&&c(!e)}}function B(e){if(o.triggerClickOnClose||e.stopPropagation(),!o.disabled){const{onClose:c}=o;c&&Po(c,e)}}const $={setTextContent(e){const{value:c}=h;c&&(c.textContent=e)}},H=mo("Tag",t,v),x=K(()=>{const{type:e,color:{color:c,textColor:C}={}}=o,d=a.value,{common:{cubicBezierEaseInOut:R},self:{padding:_,closeMargin:M,borderRadius:T,opacityDisabled:E,textColorCheckable:W,textColorHoverCheckable:w,textColorPressedCheckable:F,textColorChecked:O,colorCheckable:V,colorHoverCheckable:D,colorPressedCheckable:A,colorChecked:G,colorCheckedHover:Q,colorCheckedPressed:X,closeBorderRadius:Y,fontWeightStrong:Z,[i("colorBordered",e)]:oo,[i("closeSize",d)]:eo,[i("closeIconSize",d)]:ro,[i("fontSize",d)]:lo,[i("height",d)]:N,[i("color",e)]:ao,[i("textColor",e)]:co,[i("border",e)]:no,[i("closeIconColor",e)]:U,[i("closeIconColorHover",e)]:so,[i("closeIconColorPressed",e)]:to,[i("closeColorHover",e)]:io,[i("closeColorPressed",e)]:ho}}=s.value,P=zo(M);return{"--n-font-weight-strong":Z,"--n-avatar-size-override":`calc(${N} - 8px)`,"--n-bezier":R,"--n-border-radius":T,"--n-border":no,"--n-close-icon-size":ro,"--n-close-color-pressed":ho,"--n-close-color-hover":io,"--n-close-border-radius":Y,"--n-close-icon-color":U,"--n-close-icon-color-hover":so,"--n-close-icon-color-pressed":to,"--n-close-icon-color-disabled":U,"--n-close-margin-top":P.top,"--n-close-margin-right":P.right,"--n-close-margin-bottom":P.bottom,"--n-close-margin-left":P.left,"--n-close-size":eo,"--n-color":c||(b.value?oo:ao),"--n-color-checkable":V,"--n-color-checked":G,"--n-color-checked-hover":Q,"--n-color-checked-pressed":X,"--n-color-hover-checkable":D,"--n-color-pressed-checkable":A,"--n-font-size":lo,"--n-height":N,"--n-opacity-disabled":E,"--n-padding":_,"--n-text-color":C||co,"--n-text-color-checkable":W,"--n-text-color-checked":O,"--n-text-color-hover-checkable":w,"--n-text-color-pressed-checkable":F}}),y=l?xo("tag",K(()=>{let e="";const{type:c,color:{color:C,textColor:d}={}}=o;return e+=c[0],e+=a.value[0],C&&(e+=`a${J(C)}`),d&&(e+=`b${J(d)}`),b.value&&(e+="c"),e}),x,o):void 0;return{...$,rtlEnabled:H,mergedClsPrefix:v,contentRef:h,mergedBordered:b,handleClick:g,handleCloseClick:B,cssVars:l?void 0:x,themeClass:y?.themeClass,onRender:y?.onRender}},render(){const{mergedClsPrefix:o,rtlEnabled:h,closable:b,color:{borderColor:v}={},round:l,onRender:t,$slots:n}=this;t?.();const a=j(n.avatar,g=>g&&(m(),S("div",{class:k(`${o}-tag__avatar`)},[p(()=>g)],2))),s=j(n.icon,g=>g&&(m(),S("div",{class:k(`${o}-tag__icon`)},[p(()=>g)],2)));return m(),S("div",{class:k([`${o}-tag`,this.themeClass,{[`${o}-tag--rtl`]:h,[`${o}-tag--strong`]:this.strong,[`${o}-tag--disabled`]:this.disabled,[`${o}-tag--checkable`]:this.checkable,[`${o}-tag--checked`]:this.checkable&&this.checked,[`${o}-tag--round`]:l,[`${o}-tag--avatar`]:a,[`${o}-tag--icon`]:s,[`${o}-tag--closable`]:b}]),style:L(this.cssVars),onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},[p(()=>s||a),vo("span",{class:k(`${o}-tag__content`),ref:"contentRef"},[p(()=>this.$slots.default?.())],2),!this.checkable&&b?(m(),fo(po,{key:0,clsPrefix:o,class:k(`${o}-tag__close`),disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:l,isButtonTag:this.internalCloseIsButtonTag,absolute:!0},null,8,["clsPrefix","class","disabled","onClick","focusable","round","isButtonTag"])):p(()=>null),!this.checkable&&this.mergedBordered?(m(),S("div",{key:2,class:k(`${o}-tag__border`),style:L({borderColor:v})},null,6)):p(()=>null)],46,Mo)}});export{wo as T,Eo as t};
