import{b6 as Y,hX as G,hY as g,gR as h,b8 as I,bZ as l,bY as $,gu as K,b7 as Q,d as X,b9 as w,o as t,p as C,gw as Z,bc as q,bQ as J,c2 as U,f as H,j as ee,c as T,bb as u,bM as oe,bI as z,f8 as re,f9 as ne,gF as te,gE as le,gD as se,gG as ae,a as ie,f6 as ce,ba as A,gp as de,be as i}from"./index-BXADM5hg.js";function fe(e){const{lineHeight:s,borderRadius:v,fontWeightStrong:p,baseColor:r,dividerColor:_,actionColor:E,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:b,closeIconColor:m,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:S,fontSize:R}=e;return{...G,fontSize:R,lineHeight:s,titleFontWeight:p,borderRadius:v,border:`1px solid ${_}`,color:E,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:v,closeColorHover:f,closeColorPressed:b,closeIconColor:m,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${g(r,h(o,{alpha:.25}))}`,colorInfo:g(r,h(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:b,closeIconColorInfo:m,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${g(r,h(y,{alpha:.25}))}`,colorSuccess:g(r,h(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:b,closeIconColorSuccess:m,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${g(r,h(P,{alpha:.33}))}`,colorWarning:g(r,h(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:b,closeIconColorWarning:m,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${g(r,h(S,{alpha:.25}))}`,colorError:g(r,h(S,{alpha:.08})),titleTextColorError:d,iconColorError:S,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:b,closeIconColorError:m,closeIconColorHoverError:x,closeIconColorPressedError:n}}const ge={common:Y,self:fe};var he=I("alert",`
 line-height: var(--n-line-height);
 border-radius: var(--n-border-radius);
 position: relative;
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-color);
 text-align: start;
 word-break: break-word;
`,[l("border",`
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 transition: border-color .3s var(--n-bezier);
 border: var(--n-border);
 pointer-events: none;
 `),$("closable",[I("alert-body",[l("title",`
 padding-right: 24px;
 `)])]),l("icon",{color:"var(--n-icon-color)"}),I("alert-body",{padding:"var(--n-padding)"},[l("title",{color:"var(--n-title-text-color)"}),l("content",{color:"var(--n-content-text-color)"})]),K({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),l("icon",`
 position: absolute;
 left: 0;
 top: 0;
 align-items: center;
 justify-content: center;
 display: flex;
 width: var(--n-icon-size);
 height: var(--n-icon-size);
 font-size: var(--n-icon-size);
 margin: var(--n-icon-margin);
 `),l("close",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 position: absolute;
 right: 0;
 top: 0;
 margin: var(--n-close-margin);
 `),$("show-icon",[I("alert-body",{paddingLeft:"calc(var(--n-icon-margin-left) + var(--n-icon-size) + var(--n-icon-margin-right))"})]),$("right-adjust",[I("alert-body",{paddingRight:"calc(var(--n-close-size) + var(--n-padding) + 2px)"})]),I("alert-body",`
 border-radius: var(--n-border-radius);
 transition: border-color .3s var(--n-bezier);
 `,[l("title",`
 transition: color .3s var(--n-bezier);
 font-size: 16px;
 line-height: 19px;
 font-weight: var(--n-title-font-weight);
 `,[Q("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const ue={...w.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var Ce=X({name:"Alert",inheritAttrs:!1,props:ue,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:v,inlineThemeDisabled:p,mergedRtlRef:r}=q(e),_=w("Alert","-alert",he,ge,e,s),E=J("Alert",r,s),d=H(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=_.value,{fontSize:y,borderRadius:P,titleFontWeight:S,lineHeight:R,iconSize:W,iconMargin:k,iconMarginRtl:B,closeIconSize:L,closeBorderRadius:F,closeSize:M,closeMargin:j,closeMarginRtl:V,padding:D}=o,{type:a}=e,{left:N,right:O}=de(k);return{"--n-bezier":n,"--n-color":o[i("color",a)],"--n-close-icon-size":L,"--n-close-border-radius":F,"--n-close-color-hover":o[i("closeColorHover",a)],"--n-close-color-pressed":o[i("closeColorPressed",a)],"--n-close-icon-color":o[i("closeIconColor",a)],"--n-close-icon-color-hover":o[i("closeIconColorHover",a)],"--n-close-icon-color-pressed":o[i("closeIconColorPressed",a)],"--n-icon-color":o[i("iconColor",a)],"--n-border":o[i("border",a)],"--n-title-text-color":o[i("titleTextColor",a)],"--n-content-text-color":o[i("contentTextColor",a)],"--n-line-height":R,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":S,"--n-icon-size":W,"--n-icon-margin":k,"--n-icon-margin-rtl":B,"--n-close-size":M,"--n-close-margin":j,"--n-close-margin-rtl":V,"--n-padding":D,"--n-icon-margin-left":N,"--n-icon-margin-right":O}}),c=p?U("alert",H(()=>e.type[0]),d,e):void 0,f=ee(!0),b=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:E,mergedClsPrefix:s,mergedBordered:v,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{b()},mergedTheme:_,cssVars:p?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),C(Z,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,v={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),T("div",A({key:1},A(this.$attrs,v)),[u(()=>this.closable&&(t(),C(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),u(()=>this.bordered&&(t(),T("div",{class:z(`${e}-alert__border`)},null,2))),u(()=>this.showIcon&&(t(),T("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[u(()=>re(s.icon,()=>[(t(),C(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),C(ae,{key:3});case"info":return t(),C(se,{key:4});case"warning":return t(),C(le,{key:5});case"error":return t(),C(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ie("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[u(()=>ce(s.header,p=>{const r=p||this.title;return r?(t(),T("div",{key:2,class:z(`${e}-alert-body__title`)},[u(()=>r)],2)):null})),u(()=>s.default&&(t(),T("div",{class:z(`${e}-alert-body__content`)},[u(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{Ce as A};
